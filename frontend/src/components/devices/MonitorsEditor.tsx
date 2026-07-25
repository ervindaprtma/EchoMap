import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Trash2, PlayCircle } from "lucide-react"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import type { DeviceMonitor, MonitorKind, MonitorStatus } from "@/lib/types"

const KINDS: MonitorKind[] = ["TCP", "HTTP", "HTTPS"]
const UNIT_SECONDS: Record<string, number> = { seconds: 1, minutes: 60, hours: 3600 }
const STATUS_STYLE: Record<MonitorStatus, string> = {
  UP: "bg-green-500/15 text-green-500",
  DOWN: "bg-red-500/15 text-red-500",
  UNKNOWN: "bg-muted text-muted-foreground",
}

// Monitoring tab (Doc 4 §11): CRUD of a device's custom TCP/HTTP(S) checks.
// Admin-only surface — the device dialog itself is only reachable by Admins.
export function MonitorsEditor({ deviceId }: { deviceId: number }) {
  const qc = useQueryClient()
  const key = ["monitors", deviceId]
  const { data } = useQuery({
    queryKey: key,
    queryFn: () => api<{ items: DeviceMonitor[] }>(`/api/v1/devices/${deviceId}/monitors`),
  })

  const [label, setLabel] = useState("")
  const [kind, setKind] = useState<MonitorKind>("TCP")
  const [port, setPort] = useState("")
  const [pathOrUrl, setPathOrUrl] = useState("")
  const [interval, setInterval] = useState("60")
  const [unit, setUnit] = useState("seconds")
  const [err, setErr] = useState<string | null>(null)

  const invalidate = () => qc.invalidateQueries({ queryKey: key })
  const isHttp = kind === "HTTP" || kind === "HTTPS"

  const add = useMutation({
    mutationFn: () => {
      const seconds = Math.max(10, Math.round(Number(interval || "0") * UNIT_SECONDS[unit]))
      const body: Record<string, unknown> = {
        label: label.trim(),
        kind,
        interval_seconds: seconds,
        ...(port.trim() ? { port: Number(port) } : {}),
      }
      if (isHttp && pathOrUrl.trim()) {
        // An absolute URL is an override; a leading "/" is a path on the device.
        if (/^https?:\/\//i.test(pathOrUrl.trim())) body.url_override = pathOrUrl.trim()
        else body.path = pathOrUrl.trim()
      }
      return api(`/api/v1/devices/${deviceId}/monitors`, { method: "POST", body: JSON.stringify(body) })
    },
    onSuccess: () => {
      setErr(null); setLabel(""); setPort(""); setPathOrUrl(""); invalidate()
    },
    onError: (e) => setErr(e instanceof Error ? e.message : "Failed to add monitor"),
  })

  const del = useMutation({
    mutationFn: (id: number) => api(`/api/v1/monitors/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      api(`/api/v1/monitors/${id}`, { method: "PATCH", body: JSON.stringify({ enabled }) }),
    onSuccess: invalidate,
  })

  const monitors = data?.items ?? []

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        Extra checks beyond ping. Each runs its own debounce → alert/pop-up/sound, and pauses while the device is DOWN.
      </p>

      {monitors.length > 0 && (
        <ul className="divide-y rounded border text-sm">
          {monitors.map((m) => (
            <li key={m.id} className="flex items-center justify-between gap-2 px-3 py-1.5">
              <div className="min-w-0">
                <span className={`mr-2 inline-flex rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLE[m.status]}`}>
                  {m.status}
                </span>
                <span className="font-medium">{m.label}</span>{" "}
                <span className="text-xs text-muted-foreground">
                  {m.kind}{m.port ? ` :${m.port}` : ""}{m.url_override ? ` ${m.url_override}` : m.path ?? ""}
                  {" · every "}{m.interval_seconds}s
                  {m.last_duration_ms != null ? ` · ${Math.round(m.last_duration_ms)}ms` : ""}
                  {m.last_http_status != null ? ` · ${m.last_http_status}` : ""}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <MonitorTestButton id={m.id} />
                <Switch checked={m.enabled} onCheckedChange={(enabled) => toggle.mutate({ id: m.id, enabled })} />
                <button
                  type="button"
                  onClick={() => del.mutate(m.id)}
                  className="text-muted-foreground hover:text-red-500"
                  aria-label={`Delete ${m.label}`}
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      <div className="grid grid-cols-[1fr_auto_5rem_1fr] items-end gap-2">
        <Input placeholder="Label (e.g. API :8443)" value={label} onChange={(e) => setLabel(e.target.value)} />
        <Select value={kind} onValueChange={(v) => setKind(v as MonitorKind)}>
          <SelectTrigger className="w-28"><SelectValue /></SelectTrigger>
          <SelectContent>
            {KINDS.map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}
          </SelectContent>
        </Select>
        <Input
          placeholder={kind === "TCP" ? "port*" : "port"}
          value={port}
          onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
        />
        <Input
          placeholder={isHttp ? "/path or https://…" : "(n/a for TCP)"}
          value={pathOrUrl}
          onChange={(e) => setPathOrUrl(e.target.value)}
          disabled={!isHttp}
        />
      </div>

      <div className="flex items-end gap-2">
        <div className="flex items-center gap-1">
          <span className="text-xs text-muted-foreground">Every</span>
          <Input
            className="w-20"
            value={interval}
            onChange={(e) => setInterval(e.target.value.replace(/\D/g, ""))}
          />
          <Select value={unit} onValueChange={setUnit}>
            <SelectTrigger className="w-28"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="seconds">seconds</SelectItem>
              <SelectItem value="minutes">minutes</SelectItem>
              <SelectItem value="hours">hours</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <Button
          type="button"
          size="sm"
          className="ml-auto"
          disabled={!label.trim() || (kind === "TCP" && !port.trim()) || add.isPending}
          onClick={() => add.mutate()}
        >
          Add monitor
        </Button>
      </div>
      {err && <p className="text-xs text-red-500">{err}</p>}
    </div>
  )
}

// Runs the check once and shows the outcome inline (Doc 5 §8.4 /test).
function MonitorTestButton({ id }: { id: number }) {
  const [result, setResult] = useState<string | null>(null)
  const test = useMutation({
    mutationFn: () =>
      api<{ ok: boolean; duration_ms: number; http_status?: number; cert_days_left?: number }>(
        `/api/v1/monitors/${id}/test`,
        { method: "POST" },
      ),
    onSuccess: (r) =>
      setResult(
        `${r.ok ? "OK" : "FAIL"} ${Math.round(r.duration_ms)}ms` +
          (r.http_status ? ` (${r.http_status})` : "") +
          (r.cert_days_left != null ? ` cert ${r.cert_days_left}d` : ""),
      ),
    onError: (e) => setResult(e instanceof Error ? e.message : "error"),
  })
  return (
    <span className="flex items-center gap-1">
      {result && <span className="text-xs text-muted-foreground">{result}</span>}
      <button
        type="button"
        onClick={() => test.mutate()}
        disabled={test.isPending}
        className="text-muted-foreground hover:text-foreground"
        aria-label="Test now"
      >
        <PlayCircle className="h-4 w-4" />
      </button>
    </span>
  )
}
