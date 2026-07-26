// Shared API types (Doc 5). One place, imported by pages/components/realtime.

export type DeviceStatus = "UP" | "DOWN" | "ORPHANED" | "UNKNOWN"
export type DownReason = "PROBE" | "PARENT" | null
export type ServiceKind = "HTTP" | "HTTPS" | "SSH" | "TELNET" | "CUSTOM_URL"

export interface Device {
  id: number
  ip_address: string
  hostname: string | null
  name: string
  name_source: string
  status: DeviceStatus
  down_reason: DownReason
  is_flapping: boolean
  source_type: string
  subnet_id: number | null
  site: string | null
  vendor: string | null
  model: string | null
  icon: string
  icon_id: number | null
  parent_device_id: number | null
  map_id: number | null
  snmp_enabled: boolean
  pos_x: number | null
  pos_y: number | null
  is_locked: boolean
  last_seen_at: string | null
  last_change_at: string | null
}

export interface DeviceList {
  items: Device[]
  total: number
  page: number
  page_size: number
}

export interface MapRow {
  id: number
  parent_map_id: number | null
  name: string
  pos_x: number | null
  pos_y: number | null
}

export interface IconRow {
  id: number
  name: string
  category: string | null
  mime: string
  data_url: string
}

export interface EdgeRow {
  id: number
  source_device_id: number
  target_device_id: number
  link_type: "AUTO" | "MANUAL"
  label: string | null
}

export interface SubmapSummary {
  map_id: number
  name: string
  pos_x: number | null
  pos_y: number | null
  up: number
  down: number
  orphaned: number
  unknown: number
}

export interface TopologyResponse {
  nodes: Device[]
  edges: EdgeRow[]
  submaps: SubmapSummary[]
}

export interface DeviceService {
  id: number
  device_id: number
  label: string
  kind: ServiceKind
  port: number | null
  path: string | null
  url: string | null
}

export type MonitorKind = "TCP" | "HTTP" | "HTTPS"
export type MonitorStatus = "UP" | "DOWN" | "UNKNOWN"

// Custom per-device port/URL monitor (Doc 5 §8.4, Phase 9).
export interface DeviceMonitor {
  id: number
  device_id: number
  label: string
  kind: MonitorKind
  port: number | null
  path: string | null
  url_override: string | null
  expect_status_lo: number
  expect_status_hi: number
  interval_seconds: number
  timeout_ms: number
  enabled: boolean
  status: MonitorStatus
  last_change_at: string | null
  last_check_at: string | null
  last_duration_ms: number | null
  last_http_status: number | null
  cert_expires_at: string | null
  created_at: string
  updated_at: string
}

// ---- History metrics reads (Doc 5 §8.5–8.6, Phase 10) ----
export type HistoryRange = "1h" | "6h" | "24h" | "7d" | "30d"

export interface PingPoint {
  t: string
  latency_avg: number | null
  latency_p95: number | null
  packet_loss: number | null
  jitter: number | null
  availability: number | null
}
export interface PingMetrics {
  range: HistoryRange
  points: PingPoint[]
}

export interface MonitorPoint {
  t: string
  duration_ms: number | null
  availability: number | null
}
export interface StatusBandPoint {
  t: string
  c2xx: number
  c3xx: number
  c4xx: number
  c5xx: number
}
export interface MonitorSeries {
  kind: MonitorKind
  points: MonitorPoint[]
  status_band?: StatusBandPoint[]
  cert_days_left?: number
}
export interface MonitorMetrics {
  range: HistoryRange
  series: MonitorSeries
}

// Alert rules (Doc 5 §8) — device_id null = a global rule for every device.
export type AlertChannel = "TELEGRAM" | "EMAIL"
export interface AlertRule {
  id: number
  device_id: number | null
  device_name: string | null
  channel: AlertChannel
  target: string
  on_down: boolean
  on_up: boolean
  on_flapping: boolean
  on_orphaned: boolean
  enabled: boolean
}
