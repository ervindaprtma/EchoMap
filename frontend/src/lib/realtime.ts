import { useEffect, useRef, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import type { Device, DeviceList, DownReason, DeviceStatus, TopologyResponse } from "@/lib/types"
import { fireAlert, fireMonitorAlert, type MonitorFrame } from "@/lib/alerts"

interface StatusEvent {
  type: string
  device_id: number
  name?: string
  ip_address?: string
  from_status?: DeviceStatus
  to_status: DeviceStatus
  down_reason?: DownReason
  is_flapping: boolean
}

// device.updated: an attribute changed with no status transition — today the
// resolver repointing a domain-based device at a new IP (Doc 3 §6).
interface UpdatedEvent {
  type: string
  device_id: number
  ip_address: string
  hostname?: string
  name?: string
}

// patchDevice applies the same field update to every cache that holds devices,
// so one frame recolors/relabels the table and the canvas alike.
function patchDevice(qc: ReturnType<typeof useQueryClient>, id: number, fields: Partial<Device>) {
  qc.setQueryData<DeviceList>(["devices"], (old) =>
    old ? { ...old, items: old.items.map((d) => (d.id === id ? { ...d, ...fields } : d)) } : old,
  )
  qc.setQueriesData<TopologyResponse>({ queryKey: ["topology"] }, (old) =>
    old ? { ...old, nodes: old.nodes.map((d) => (d.id === id ? { ...d, ...fields } : d)) } : old,
  )
}

// useRealtime opens the /ws stream and patches the ["devices"] cache in place on
// each device.status event, so status badges recolor without a refetch. Returns
// the live connection state (for a header indicator). Auto-reconnects on drop.
export function useRealtime(): boolean {
  const qc = useQueryClient()
  const [connected, setConnected] = useState(false)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    let ws: WebSocket | null = null
    let retry: ReturnType<typeof setTimeout> | undefined
    let topoRefetch: ReturnType<typeof setTimeout> | undefined

    const connect = () => {
      // The session cookie authenticates the upgrade (same-origin via the dev
      // proxy / prod reverse proxy), so no token travels in the URL (Phase 8).
      const proto = location.protocol === "https:" ? "wss" : "ws"
      ws = new WebSocket(`${proto}://${location.host}/ws`)

      ws.onopen = () => setConnected(true)
      ws.onmessage = (e) => {
        let ev: StatusEvent | UpdatedEvent
        try {
          ev = JSON.parse(e.data)
        } catch {
          return
        }

        if (ev.type === "device.updated") {
          const u = ev as UpdatedEvent
          patchDevice(qc, u.device_id, { ip_address: u.ip_address })
          return
        }
        if (ev.type === "monitor.status") {
          const m = ev as unknown as MonitorFrame & { device_id: number }
          // Refresh the device's monitor list (the Monitoring tab shows live status)
          // and sound/pop-up per this browser's prefs.
          qc.invalidateQueries({ queryKey: ["monitors", m.device_id] })
          fireMonitorAlert(m)
          return
        }
        if (ev.type !== "device.status") return

        const s = ev as StatusEvent
        // Recolor table rows and topology nodes in place (cascade events arrive
        // one frame per child), then refetch lazily so submap aggregate badges
        // catch up. The refetch is debounced: a parent with N children emits N
        // frames in a burst, and N invalidations would mean N refetches for one
        // incident.
        patchDevice(qc, s.device_id, {
          status: s.to_status,
          down_reason: s.down_reason ?? null,
          is_flapping: s.is_flapping,
        })
        clearTimeout(topoRefetch)
        topoRefetch = setTimeout(() => qc.invalidateQueries({ queryKey: ["topology"] }), 500)

        // Sound / desktop pop-up per this browser's prefs (Doc 4 §6).
        fireAlert(s)
      }
      ws.onclose = () => {
        setConnected(false)
        if (alive.current) retry = setTimeout(connect, 2000)
      }
      ws.onerror = () => ws?.close()
    }

    connect()
    return () => {
      alive.current = false
      if (retry) clearTimeout(retry)
      if (topoRefetch) clearTimeout(topoRefetch)
      ws?.close()
    }
  }, [qc])

  return connected
}
