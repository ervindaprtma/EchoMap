import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api, ApiError } from "@/lib/api"
import { useAuth, type Role } from "@/lib/auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"

interface User {
  id: number
  username: string
  role: Role
  enabled: boolean
  must_change_password: boolean
  created_at: string
  last_login_at: string | null
}

const ROLES: Role[] = ["OPERATOR", "ADMIN", "SUPERADMIN"]

export default function Users() {
  const [err, setErr] = useState<string | null>(null)
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">Users &amp; Sessions</h2>
      {err && (
        <p className="rounded border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">{err}</p>
      )}
      <Tabs defaultValue="users">
        <TabsList>
          <TabsTrigger value="users">Users</TabsTrigger>
          <TabsTrigger value="sessions">Sessions</TabsTrigger>
        </TabsList>
        <TabsContent value="users">
          <UsersTab onError={setErr} />
        </TabsContent>
        <TabsContent value="sessions">
          <SessionsTab onError={setErr} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function useReport(onError: (m: string | null) => void) {
  return (e: unknown) => onError(e instanceof ApiError || e instanceof Error ? e.message : "Request failed")
}

function UsersTab({ onError }: { onError: (m: string | null) => void }) {
  const qc = useQueryClient()
  const { me } = useAuth()
  const report = useReport(onError)
  const { data } = useQuery<{ items: User[] }>({ queryKey: ["users"], queryFn: () => api("/api/v1/users") })

  const invalidate = () => qc.invalidateQueries({ queryKey: ["users"] })
  const patch = useMutation({
    mutationFn: ({ id, body }: { id: number; body: Record<string, unknown> }) =>
      api(`/api/v1/users/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
    onSuccess: () => { onError(null); invalidate() },
    onError: report,
  })
  const del = useMutation({
    mutationFn: (id: number) => api(`/api/v1/users/${id}`, { method: "DELETE" }),
    onSuccess: () => { onError(null); invalidate() },
    onError: report,
  })

  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <AddUserDialog onDone={() => { onError(null); invalidate() }} onError={onError} />
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Username</TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Enabled</TableHead>
            <TableHead>Last login</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {data?.items?.map((u) => {
            const self = me?.id === u.id
            return (
              <TableRow key={u.id}>
                <TableCell className="font-medium">
                  {u.username}
                  {self && <span className="ml-1 text-xs text-muted-foreground">(you)</span>}
                  {u.must_change_password && <Badge variant="secondary" className="ml-2">must change pw</Badge>}
                </TableCell>
                <TableCell>
                  <Select value={u.role} onValueChange={(role) => patch.mutate({ id: u.id, body: { role } })}>
                    <SelectTrigger className="h-8 w-36"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {ROLES.map((r) => <SelectItem key={r} value={r}>{r}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  <Switch checked={u.enabled} onCheckedChange={(enabled) => patch.mutate({ id: u.id, body: { enabled } })} />
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">
                  {u.last_login_at ? new Date(u.last_login_at).toLocaleString() : "never"}
                </TableCell>
                <TableCell className="space-x-1 text-right">
                  <ResetPasswordDialog user={u} onDone={() => onError(null)} onError={onError} />
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-destructive"
                    disabled={self}
                    onClick={() => {
                      if (confirm(`Delete user "${u.username}"? This cannot be undone.`)) del.mutate(u.id)
                    }}
                  >
                    Delete
                  </Button>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

function AddUserDialog({ onDone, onError }: { onDone: () => void; onError: (m: string | null) => void }) {
  const [open, setOpen] = useState(false)
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [role, setRole] = useState<Role>("OPERATOR")
  const report = useReport(onError)
  const create = useMutation({
    mutationFn: () => api("/api/v1/users", { method: "POST", body: JSON.stringify({ username, password, role }) }),
    onSuccess: () => {
      setOpen(false); setUsername(""); setPassword(""); setRole("OPERATOR"); onDone()
    },
    onError: report,
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">Add User</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>Add user</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="nu">Username</Label>
            <Input id="nu" value={username} onChange={(e) => setUsername(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="np">Initial password (min 8 chars)</Label>
            <Input id="np" type="text" value={password} onChange={(e) => setPassword(e.target.value)} />
            <button
              type="button"
              className="text-xs text-muted-foreground underline"
              onClick={() => setPassword(Math.random().toString(36).slice(2) + "A1!")}
            >
              generate
            </button>
          </div>
          <div className="space-y-1.5">
            <Label>Role</Label>
            <Select value={role} onValueChange={(v) => setRole(v as Role)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {ROLES.map((r) => <SelectItem key={r} value={r}>{r}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <Button onClick={() => create.mutate()} disabled={create.isPending} className="w-full">
            {create.isPending ? "Creating…" : "Create user"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ResetPasswordDialog({ user, onDone, onError }: { user: User; onDone: () => void; onError: (m: string | null) => void }) {
  const [open, setOpen] = useState(false)
  const [password, setPassword] = useState("")
  const report = useReport(onError)
  const reset = useMutation({
    mutationFn: () => api(`/api/v1/users/${user.id}`, { method: "PATCH", body: JSON.stringify({ password }) }),
    onSuccess: () => { setOpen(false); setPassword(""); onDone() },
    onError: report,
  })
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="ghost">Reset pw</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>Reset password — {user.username}</DialogTitle></DialogHeader>
        <p className="text-sm text-muted-foreground">
          The user will be required to change it on next login, and their other sessions are revoked.
        </p>
        <div className="space-y-3">
          <Input type="text" placeholder="New password (min 8 chars)" value={password} onChange={(e) => setPassword(e.target.value)} />
          <Button onClick={() => reset.mutate()} disabled={reset.isPending || password.length < 8} className="w-full">
            {reset.isPending ? "Saving…" : "Set password"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

interface Session {
  id: number
  username: string
  ip: string | null
  user_agent: string | null
  created_at: string
  last_seen_at: string
  current: boolean
}

function SessionsTab({ onError }: { onError: (m: string | null) => void }) {
  const qc = useQueryClient()
  const report = useReport(onError)
  const { data } = useQuery<{ items: Session[] }>({
    queryKey: ["sessions", "all"],
    queryFn: () => api("/api/v1/sessions?scope=all"),
  })
  const revoke = useMutation({
    mutationFn: (id: number) => api(`/api/v1/sessions/${id}`, { method: "DELETE" }),
    onSuccess: () => { onError(null); qc.invalidateQueries({ queryKey: ["sessions", "all"] }) },
    onError: report,
  })

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>User</TableHead>
          <TableHead>IP</TableHead>
          <TableHead>User agent</TableHead>
          <TableHead>Last seen</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {data?.items?.map((s) => (
          <TableRow key={s.id}>
            <TableCell className="font-medium">
              {s.username} {s.current && <Badge variant="secondary">this device</Badge>}
            </TableCell>
            <TableCell>{s.ip ?? "—"}</TableCell>
            <TableCell className="max-w-xs truncate text-xs text-muted-foreground">{s.user_agent ?? "—"}</TableCell>
            <TableCell className="text-sm text-muted-foreground">{new Date(s.last_seen_at).toLocaleString()}</TableCell>
            <TableCell className="text-right">
              <Button size="sm" variant="ghost" disabled={s.current} onClick={() => revoke.mutate(s.id)}>Revoke</Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
