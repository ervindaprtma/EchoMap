import { memo } from "react"
import { Handle, Position, type NodeProps } from "reactflow"
import { Link2, Monitor, Network, Router, Server, Shield } from "lucide-react"
import { statusStyle } from "@/lib/statusStyle"
import type { Device } from "@/lib/types"
import { NodeContextMenu } from "./NodeContextMenu"

export interface DeviceNodeData {
  device: Device
  iconUrl?: string // custom icon (data: URL); falls back to the builtin glyph
  websshBase: string
  onEdit: (d: Device) => void
}

const BUILTIN: Record<string, typeof Router> = {
  router: Router,
  switch: Network,
  firewall: Shield,
  server: Server,
  host: Monitor,
}

export const DeviceNode = memo(function DeviceNode({ data }: NodeProps<DeviceNodeData>) {
  const d = data.device
  const st = statusStyle(d.status, d.down_reason)
  const Icon = BUILTIN[d.icon] ?? Monitor

  return (
    <NodeContextMenu device={d} websshBase={data.websshBase} onEdit={data.onEdit}>
      <div
        title={st.viaParent ? "DOWN via dependency parent" : d.status}
        className={`relative flex w-36 flex-col items-center gap-1 rounded-md border-2 bg-background p-2 shadow-sm ${st.border}`}
      >
        <Handle type="target" position={Position.Top} className="!h-2 !w-2 !bg-muted-foreground" />
        <span className={`absolute left-1.5 top-1.5 h-2 w-2 rounded-full ${st.dot}`} />
        {st.viaParent && <Link2 className="absolute right-1.5 top-1.5 h-3.5 w-3.5 text-red-400" />}
        {data.iconUrl ? (
          <img src={data.iconUrl} alt="" className="h-8 w-8 object-contain" />
        ) : (
          <Icon className="h-8 w-8" />
        )}
        <span className="max-w-full truncate text-xs font-medium">{d.name}</span>
        <span className="font-mono text-[10px] text-muted-foreground">{d.ip_address}</span>
        <Handle type="source" position={Position.Bottom} className="!h-2 !w-2 !bg-muted-foreground" />
      </div>
    </NodeContextMenu>
  )
})
