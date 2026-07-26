// History Charts (Doc 4 §12, Phase 10). /devices/:id/history — a range Select
// plus one Card group per monitor (ping first, then each custom monitor), all
// sharing the selected time axis. Read-only; data comes from the metrics reads
// (Doc 5 §8.5–8.6). Empty windows render an explicit "no samples" state.
import { useParams, useSearchParams, Link } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import {
  ResponsiveContainer, LineChart, Line, AreaChart, Area, BarChart, Bar,
  XAxis, YAxis, CartesianGrid, Tooltip, Legend,
} from "recharts"
import { api } from "@/lib/api"
import type {
  Device, DeviceMonitor, HistoryRange, PingMetrics, MonitorMetrics,
} from "@/lib/types"
import {
  Card, CardHeader, CardTitle, CardDescription, CardContent,
} from "@/components/ui/card"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"

const RANGES: HistoryRange[] = ["1h", "6h", "24h", "7d", "30d"]
const isWide = (r: HistoryRange) => r === "7d" || r === "30d"

// Chart colors — status hues match STATUS_STYLES / the app's tailwind palette.
const C = {
  latency: "#3b82f6", p95: "#94a3b8", loss: "#ef4444", jitter: "#f59e0b",
  duration: "#3b82f6", up: "#22c55e",
  c2xx: "#22c55e", c3xx: "#3b82f6", c4xx: "#f59e0b", c5xx: "#ef4444",
}
const axis = { stroke: "hsl(var(--muted-foreground))", fontSize: 11 }
const grid = "hsl(var(--border))"
const tooltipStyle = {
  background: "hsl(var(--popover))",
  border: "1px solid hsl(var(--border))",
  borderRadius: 6,
  color: "hsl(var(--popover-foreground))",
  fontSize: 12,
}

function fmtTick(iso: string, r: HistoryRange) {
  const d = new Date(iso)
  return isWide(r)
    ? d.toLocaleDateString([], { month: "numeric", day: "numeric" })
    : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

function Empty() {
  return (
    <div className="flex h-[200px] items-center justify-center text-sm text-muted-foreground">
      no samples in this window
    </div>
  )
}

// Shared chart frame: an empty-state guard + a fixed-height ResponsiveContainer.
function Frame({ data, children, height = 200 }: {
  data: unknown[]; height?: number
  children: React.ReactNode
}) {
  if (!data.length) return <Empty />
  return (
    <ResponsiveContainer width="100%" height={height}>
      {/* children is the concrete chart; it renders its own series */}
      {children as React.ReactElement}
    </ResponsiveContainer>
  )
}

// Common axis/grid/tooltip props reused by every chart.
function common(range: HistoryRange) {
  return {
    xProps: { dataKey: "t", tickFormatter: (v: string) => fmtTick(v, range), tick: axis, minTickGap: 32 },
    yProps: { tick: axis, width: 40 },
    gridEl: <CartesianGrid stroke={grid} strokeDasharray="3 3" vertical={false} />,
    tipEl: <Tooltip contentStyle={tooltipStyle} labelFormatter={(v: string) => new Date(v).toLocaleString()} />,
  }
}

// A thin green/red uptime strip (Uptime-Kuma style) from per-bucket availability%.
function AvailabilityStrip({ data }: { data: { t: string; availability: number | null }[] }) {
  if (!data.length) return null
  return (
    <div className="mt-3">
      <div className="mb-1 text-xs text-muted-foreground">Availability</div>
      <div className="flex h-4 w-full gap-px overflow-hidden rounded">
        {data.map((p) => {
          const a = p.availability
          const color = a == null ? "bg-muted" : a >= 99.5 ? "bg-green-500" : a > 0 ? "bg-amber-500" : "bg-red-500"
          return (
            <div key={p.t} className={`h-full flex-1 ${color}`}
              title={`${new Date(p.t).toLocaleString()} — ${a == null ? "no data" : a.toFixed(1) + "%"}`} />
          )
        })}
      </div>
    </div>
  )
}

function ChartBlock({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 text-xs font-medium text-muted-foreground">{title}</div>
      {children}
    </div>
  )
}

function PingGroup({ deviceId, range }: { deviceId: number; range: HistoryRange }) {
  const q = useQuery({
    queryKey: ["metrics", "ping", deviceId, range],
    queryFn: () => api<PingMetrics>(`/api/v1/devices/${deviceId}/metrics/ping?range=${range}`),
  })
  const pts = q.data?.points ?? []
  const { xProps, yProps, gridEl, tipEl } = common(range)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Ping (ICMP)</CardTitle>
        <CardDescription>Latency, packet loss, jitter & availability</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <ChartBlock title="Latency (ms) — avg & p95">
          <Frame data={pts}>
            <LineChart data={pts}>
              {gridEl}<XAxis {...xProps} /><YAxis {...yProps} />{tipEl}<Legend />
              <Line type="monotone" dataKey="latency_avg" name="avg" stroke={C.latency} dot={false} connectNulls={false} />
              <Line type="monotone" dataKey="latency_p95" name="p95" stroke={C.p95} strokeDasharray="4 2" dot={false} connectNulls={false} />
            </LineChart>
          </Frame>
        </ChartBlock>

        <ChartBlock title="Packet loss (%)">
          <Frame data={pts} height={140}>
            <AreaChart data={pts}>
              {gridEl}<XAxis {...xProps} /><YAxis {...yProps} domain={[0, 100]} />{tipEl}
              <Area type="monotone" dataKey="packet_loss" stroke={C.loss} fill={C.loss} fillOpacity={0.2} connectNulls={false} />
            </AreaChart>
          </Frame>
        </ChartBlock>

        <ChartBlock title="Jitter (ms) — RFC 3550">
          <Frame data={pts} height={140}>
            <LineChart data={pts}>
              {gridEl}<XAxis {...xProps} /><YAxis {...yProps} />{tipEl}
              <Line type="monotone" dataKey="jitter" stroke={C.jitter} dot={false} connectNulls={false} />
            </LineChart>
          </Frame>
        </ChartBlock>

        <AvailabilityStrip data={pts} />
      </CardContent>
    </Card>
  )
}

function CertBadge({ days }: { days: number }) {
  const tone = days < 7 ? "bg-red-500/15 text-red-500" : days < 30 ? "bg-amber-500/15 text-amber-500" : "bg-green-500/15 text-green-500"
  return <span className={`rounded px-2 py-0.5 text-xs font-medium ${tone}`}>TLS expires in {days} day{days === 1 ? "" : "s"}</span>
}

function MonitorGroup({ monitor, range }: { monitor: DeviceMonitor; range: HistoryRange }) {
  const q = useQuery({
    queryKey: ["metrics", "monitor", monitor.id, range],
    queryFn: () => api<MonitorMetrics>(`/api/v1/monitors/${monitor.id}/metrics?range=${range}`),
  })
  const s = q.data?.series
  const pts = s?.points ?? []
  const band = s?.status_band ?? []
  const isHTTP = monitor.kind === "HTTP" || monitor.kind === "HTTPS"
  const { xProps, yProps, gridEl, tipEl } = common(range)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {monitor.label}
          <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">{monitor.kind}</span>
          {monitor.kind === "HTTPS" && s?.cert_days_left != null && <CertBadge days={s.cert_days_left} />}
        </CardTitle>
        <CardDescription>{isHTTP ? "Response time, status codes & availability" : "Connect time & availability"}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <ChartBlock title={isHTTP ? "Response time (ms)" : "Connect time (ms)"}>
          <Frame data={pts}>
            <LineChart data={pts}>
              {gridEl}<XAxis {...xProps} /><YAxis {...yProps} />{tipEl}
              <Line type="monotone" dataKey="duration_ms" stroke={C.duration} dot={false} connectNulls={false} />
            </LineChart>
          </Frame>
        </ChartBlock>

        {isHTTP && (
          <ChartBlock title="Status codes (count per bucket)">
            <Frame data={band} height={140}>
              <BarChart data={band}>
                {gridEl}<XAxis {...xProps} /><YAxis {...yProps} allowDecimals={false} />{tipEl}<Legend />
                <Bar dataKey="c2xx" name="2xx" stackId="s" fill={C.c2xx} />
                <Bar dataKey="c3xx" name="3xx" stackId="s" fill={C.c3xx} />
                <Bar dataKey="c4xx" name="4xx" stackId="s" fill={C.c4xx} />
                <Bar dataKey="c5xx" name="5xx" stackId="s" fill={C.c5xx} />
              </BarChart>
            </Frame>
          </ChartBlock>
        )}

        <AvailabilityStrip data={pts} />
      </CardContent>
    </Card>
  )
}

export function History() {
  const { id } = useParams()
  const deviceId = Number(id)
  const [params, setParams] = useSearchParams()
  const range = (params.get("range") as HistoryRange) || "24h"
  const setRange = (r: string) => setParams({ range: r }, { replace: true })

  const device = useQuery({
    queryKey: ["device", deviceId],
    queryFn: () => api<Device>(`/api/v1/devices/${deviceId}`),
    enabled: Number.isFinite(deviceId),
  })
  const monitors = useQuery({
    queryKey: ["monitors", deviceId],
    queryFn: () => api<DeviceMonitor[]>(`/api/v1/devices/${deviceId}/monitors`),
    enabled: Number.isFinite(deviceId),
  })

  if (!Number.isFinite(deviceId)) return <div className="p-6 text-sm text-muted-foreground">Invalid device.</div>

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Link to="/" className="text-sm text-muted-foreground hover:text-foreground">← Devices</Link>
          <h1 className="text-xl font-semibold">
            History — {device.data?.name ?? `#${deviceId}`}
            {device.data?.ip_address && <span className="ml-2 text-sm font-normal text-muted-foreground">{device.data.ip_address}</span>}
          </h1>
        </div>
        <Select value={range} onValueChange={setRange}>
          <SelectTrigger className="h-9 w-28"><SelectValue /></SelectTrigger>
          <SelectContent>
            {RANGES.map((r) => <SelectItem key={r} value={r}>{r}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <PingGroup deviceId={deviceId} range={range} />
      {(monitors.data ?? []).map((m) => <MonitorGroup key={m.id} monitor={m} range={range} />)}
    </div>
  )
}
