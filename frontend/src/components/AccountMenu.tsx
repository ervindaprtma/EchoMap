import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { LogOut, KeyRound, MonitorSmartphone, UserCircle2 } from "lucide-react"
import { api } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { ChangePasswordForm } from "@/components/ChangePasswordForm"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"

interface Session {
  id: number
  ip: string | null
  user_agent: string | null
  created_at: string
  last_seen_at: string
  current: boolean
}

function SessionsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient()
  const { data } = useQuery<{ items: Session[] }>({
    queryKey: ["sessions"],
    queryFn: () => api("/api/v1/sessions"),
    enabled: open,
  })
  const revoke = useMutation({
    mutationFn: (id: number) => api(`/api/v1/sessions/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Active sessions</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          {data?.items?.length ? (
            data.items.map((s) => (
              <div key={s.id} className="flex items-center justify-between rounded border p-2 text-sm">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{s.ip ?? "unknown IP"}</span>
                    {s.current && <Badge variant="secondary">this device</Badge>}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">{s.user_agent ?? "—"}</div>
                  <div className="text-xs text-muted-foreground">
                    last seen {new Date(s.last_seen_at).toLocaleString()}
                  </div>
                </div>
                {!s.current && (
                  <Button size="sm" variant="ghost" onClick={() => revoke.mutate(s.id)} disabled={revoke.isPending}>
                    Revoke
                  </Button>
                )}
              </div>
            ))
          ) : (
            <p className="text-sm text-muted-foreground">No other sessions.</p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

export function AccountMenu() {
  const { me, logout } = useAuth()
  const [pwOpen, setPwOpen] = useState(false)
  const [sessOpen, setSessOpen] = useState(false)
  if (!me) return null

  return (
    <>
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="ghost" size="sm" className="gap-1.5">
            <UserCircle2 className="h-4 w-4" />
            {me.username}
          </Button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-56 p-2">
          <div className="px-2 py-1.5">
            <div className="text-sm font-medium">{me.username}</div>
            <Badge variant="secondary" className="mt-1">{me.role}</Badge>
          </div>
          <div className="my-1 h-px bg-border" />
          <Button variant="ghost" size="sm" className="w-full justify-start gap-2" onClick={() => setPwOpen(true)}>
            <KeyRound className="h-4 w-4" /> Change password
          </Button>
          <Button variant="ghost" size="sm" className="w-full justify-start gap-2" onClick={() => setSessOpen(true)}>
            <MonitorSmartphone className="h-4 w-4" /> Active sessions
          </Button>
          <div className="my-1 h-px bg-border" />
          <Button variant="ghost" size="sm" className="w-full justify-start gap-2 text-destructive" onClick={() => void logout()}>
            <LogOut className="h-4 w-4" /> Sign out
          </Button>
        </PopoverContent>
      </Popover>

      <Dialog open={pwOpen} onOpenChange={setPwOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Change password</DialogTitle>
          </DialogHeader>
          <ChangePasswordForm onDone={() => setPwOpen(false)} />
        </DialogContent>
      </Dialog>

      <SessionsDialog open={sessOpen} onOpenChange={setSessOpen} />
    </>
  )
}
