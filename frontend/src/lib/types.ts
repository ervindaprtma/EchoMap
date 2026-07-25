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
