import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Trash2 } from "lucide-react"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import type { AlertChannel, AlertRule, DeviceList } from "@/lib/types"

const CHANNELS: AlertChannel[] = ["TELEGRAM", "EMAIL"]
// Radix Select forbids an empty-string item value, so a global rule uses this
// sentinel that maps back to device_id: null on submit.
const GLOBAL = "__global__"

// Settings page (Doc 4 §6). Admin surface; today it hosts only the Alerts tab
// (alert rules + delivery channels). Netbox/SNMP/Templates/Polling tabs land
// with their own slices — add a <TabsTrigger>/<TabsContent> pair each.
export default function Settings() {
  return (
    <div className="space-y-6">
      <h2 className="text-2xl font-bold tracking-tight">Settings</h2>
      <Tabs defaultValue="alerts">
        <TabsList>
          <TabsTrigger value="alerts">Alerts</TabsTrigger>
        </TabsList>
        <TabsContent value="alerts" className="space-y-6">
          <ChannelsCard />
          <AlertRulesCard />
        </TabsContent>
      </Tabs>
    </div>
  )
}

// Delivery credentials. Reuses the already-built GET/PUT /settings/channels
// (Phase 7). The bot token is write-only: GET returns •••••••• when one is set,
// and echoing that back leaves it unchanged.
interface ChannelData {
  telegram_bot_token: string
  telegram_template: string
  webssh_url: string
  email_subject_template: string
  email_body_template: string
  smtp: { host: string; port: number; username: string; from: string; tls: string; password: string }
}
const TLS_MODES = ["none", "starttls", "tls"]

function ChannelsCard() {
  const { data } = useQuery({
    queryKey: ["settings", "channels"],
    queryFn: () => api<ChannelData>("/api/v1/settings/channels"),
  })
  const [token, setToken] = useState<string | null>(null) // null = untouched
  const [webssh, setWebssh] = useState<string | null>(null)
  // SMTP fields: null = untouched (fall back to fetched value).
  const [host, setHost] = useState<string | null>(null)
  const [port, setPort] = useState<string | null>(null)
  const [user, setUser] = useState<string | null>(null)
  const [from, setFrom] = useState<string | null>(null)
  const [tls, setTls] = useState<string | null>(null)
  const [pw, setPw] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  const s = data?.smtp
  const cur = <T,>(v: T | null, fallback: T) => (v !== null ? v : fallback)
  const smtpTouched = [host, port, user, from, tls, pw].some((v) => v !== null)

  const save = useMutation({
    mutationFn: () =>
      api("/api/v1/settings/channels", {
        method: "PUT",
        body: JSON.stringify({
          ...(token !== null ? { telegram_bot_token: token } : {}),
          ...(webssh !== null ? { webssh_url: webssh } : {}),
          ...(smtpTouched
            ? {
                smtp: {
                  host: cur(host, s?.host ?? ""),
                  port: Number(cur(port, String(s?.port ?? 0))) || 0,
                  username: cur(user, s?.username ?? ""),
                  from: cur(from, s?.from ?? ""),
                  tls: cur(tls, s?.tls || "none"),
                  // masked echo (password unchanged) keeps the stored value.
                  password: pw !== null ? pw : (s?.password ?? ""),
                },
              }
            : {}),
        }),
      }),
    onSuccess: () => {
      setSaved(true)
      setTimeout(() => setSaved(false), 1500)
    },
  })

  const [testTo, setTestTo] = useState("")
  const [testMsg, setTestMsg] = useState<string | null>(null)
  const test = useMutation({
    mutationFn: () =>
      api("/api/v1/settings/channels/test-email", { method: "POST", body: JSON.stringify({ to: testTo.trim() }) }),
    onSuccess: () => setTestMsg("Sent — check the inbox."),
    onError: (e) => setTestMsg(e instanceof Error ? e.message : "Send failed"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Delivery channels</CardTitle>
        <CardDescription>
          Credentials the notifier uses to send alerts. Secrets (Telegram token, SMTP password) are stored write-only.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-1.5">
          <Label htmlFor="tg-token">Telegram bot token</Label>
          <Input
            id="tg-token"
            type="password"
            placeholder={data?.telegram_bot_token || "not set"}
            value={token ?? ""}
            onChange={(e) => setToken(e.target.value)}
          />
        </div>

        <div className="space-y-2 rounded-md border p-3">
          <p className="text-sm font-medium">SMTP (email alerts)</p>
          <div className="grid grid-cols-[1fr_6rem] gap-2">
            <div className="grid gap-1.5">
              <Label className="text-xs text-muted-foreground">Host</Label>
              <Input placeholder="smtp.example.net" value={cur(host, s?.host ?? "")} onChange={(e) => setHost(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label className="text-xs text-muted-foreground">Port</Label>
              <Input
                placeholder="587"
                value={cur(port, String(s?.port ?? ""))}
                onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
              />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div className="grid gap-1.5">
              <Label className="text-xs text-muted-foreground">Username</Label>
              <Input placeholder="(optional)" value={cur(user, s?.username ?? "")} onChange={(e) => setUser(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label className="text-xs text-muted-foreground">Password</Label>
              <Input
                type="password"
                placeholder={s?.password || "not set"}
                value={pw ?? ""}
                onChange={(e) => setPw(e.target.value)}
              />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div className="grid gap-1.5">
              <Label className="text-xs text-muted-foreground">From address</Label>
              <Input placeholder="alerts@example.net" value={cur(from, s?.from ?? "")} onChange={(e) => setFrom(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label className="text-xs text-muted-foreground">Encryption</Label>
              <Select value={cur(tls, s?.tls || "none")} onValueChange={setTls}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {TLS_MODES.map((m) => <SelectItem key={m} value={m}>{m}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="flex items-end gap-2">
            <div className="grid flex-1 gap-1.5">
              <Label className="text-xs text-muted-foreground">Send a test email to</Label>
              <Input placeholder="you@example.net" value={testTo} onChange={(e) => setTestTo(e.target.value)} />
            </div>
            <Button size="sm" variant="secondary" disabled={!testTo.trim() || test.isPending} onClick={() => { setTestMsg(null); test.mutate() }}>
              Send test
            </Button>
          </div>
          {testMsg && <p className={`text-xs ${test.isError ? "text-red-500" : "text-green-500"}`}>{testMsg}</p>}
          <p className="text-xs text-muted-foreground">
            Save first, then test. Add an <span className="font-medium">EMAIL</span> alert rule below (target = recipient address) for alerts to fire.
          </p>
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="webssh">Web-SSH gateway URL (optional)</Label>
          <Input
            id="webssh"
            placeholder="https://webssh.example.net/"
            value={webssh ?? data?.webssh_url ?? ""}
            onChange={(e) => setWebssh(e.target.value)}
          />
        </div>
        <div className="flex items-center gap-3">
          <Button
            size="sm"
            disabled={save.isPending || (token === null && webssh === null && !smtpTouched)}
            onClick={() => save.mutate()}
          >
            Save channels
          </Button>
          {saved && <span className="text-xs text-green-500">Saved</span>}
          {save.isError && <span className="text-xs text-red-500">{(save.error as Error)?.message ?? "Save failed"}</span>}
        </div>
      </CardContent>
    </Card>
  )
}

// CRUD of alert_rules — the audit gap (rules were SQL-only). A rule with no
// device is global; per-event switches gate which transitions fire it.
function AlertRulesCard() {
  const qc = useQueryClient()
  const key = ["alert-rules"]
  const invalidate = () => qc.invalidateQueries({ queryKey: key })

  const { data } = useQuery({ queryKey: key, queryFn: () => api<{ items: AlertRule[] }>("/api/v1/alert-rules") })
  const devices = useQuery({
    queryKey: ["devices", "picker"],
    queryFn: () => api<DeviceList>("/api/v1/devices?page_size=200"),
  })

  const [deviceId, setDeviceId] = useState(GLOBAL) // GLOBAL sentinel = all devices
  const [channel, setChannel] = useState<AlertChannel>("TELEGRAM")
  const [target, setTarget] = useState("")
  const [err, setErr] = useState<string | null>(null)

  const add = useMutation({
    mutationFn: () =>
      api("/api/v1/alert-rules", {
        method: "POST",
        body: JSON.stringify({
          device_id: deviceId === GLOBAL ? null : Number(deviceId),
          channel,
          target: target.trim(),
        }),
      }),
    onSuccess: () => { setErr(null); setTarget(""); invalidate() },
    onError: (e) => setErr(e instanceof Error ? e.message : "Failed to add rule"),
  })
  const patch = useMutation({
    mutationFn: ({ id, body }: { id: number; body: Record<string, unknown> }) =>
      api(`/api/v1/alert-rules/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
    onSuccess: invalidate,
  })
  const del = useMutation({
    mutationFn: (id: number) => api(`/api/v1/alert-rules/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  })

  const rules = data?.items ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle>Alert rules</CardTitle>
        <CardDescription>
          Who gets notified, and for which events. A rule with no device applies to every device.
          TELEGRAM and EMAIL both deliver (configure their channel above).
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {rules.length > 0 && (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Scope</TableHead>
                  <TableHead>Channel</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead className="text-center">Down</TableHead>
                  <TableHead className="text-center">Up</TableHead>
                  <TableHead className="text-center">Flap</TableHead>
                  <TableHead className="text-center">Orph</TableHead>
                  <TableHead className="text-center">On</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule) => {
                  const cell = (field: keyof AlertRule) => (
                    <TableCell className="text-center">
                      <Switch
                        checked={rule[field] as boolean}
                        onCheckedChange={(v) => patch.mutate({ id: rule.id, body: { [field]: v } })}
                      />
                    </TableCell>
                  )
                  return (
                    <TableRow key={rule.id} className={rule.enabled ? "" : "opacity-50"}>
                      <TableCell className="font-medium">{rule.device_name ?? "All devices"}</TableCell>
                      <TableCell>{rule.channel}</TableCell>
                      <TableCell className="max-w-[16rem] truncate font-mono text-xs">{rule.target}</TableCell>
                      {cell("on_down")}
                      {cell("on_up")}
                      {cell("on_flapping")}
                      {cell("on_orphaned")}
                      {cell("enabled")}
                      <TableCell>
                        <button
                          type="button"
                          onClick={() => del.mutate(rule.id)}
                          className="text-muted-foreground hover:text-red-500"
                          aria-label="Delete rule"
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}

        <div className="grid grid-cols-[1fr_auto_1fr_auto] items-end gap-2">
          <div className="grid gap-1.5">
            <Label className="text-xs text-muted-foreground">Device</Label>
            <Select value={deviceId} onValueChange={setDeviceId}>
              <SelectTrigger><SelectValue placeholder="All devices" /></SelectTrigger>
              <SelectContent>
                <SelectItem value={GLOBAL}>All devices (global)</SelectItem>
                {(devices.data?.items ?? []).map((d) => (
                  <SelectItem key={d.id} value={String(d.id)}>{d.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <Label className="text-xs text-muted-foreground">Channel</Label>
            <Select value={channel} onValueChange={(v) => setChannel(v as AlertChannel)}>
              <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
              <SelectContent>
                {CHANNELS.map((c) => <SelectItem key={c} value={c}>{c}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <Label className="text-xs text-muted-foreground">
              {channel === "TELEGRAM" ? "Chat ID" : "Email address"}
            </Label>
            <Input
              placeholder={channel === "TELEGRAM" ? "-1001234567890" : "ops@example.net"}
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            />
          </div>
          <Button size="sm" disabled={!target.trim() || add.isPending} onClick={() => add.mutate()}>
            Add rule
          </Button>
        </div>
        {err && <p className="text-xs text-red-500">{err}</p>}
      </CardContent>
    </Card>
  )
}
