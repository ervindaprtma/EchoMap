import { memo } from "react"
import { type NodeProps } from "reactflow"
import { Folder } from "lucide-react"
import { statusStyle, worstStatus } from "@/lib/statusStyle"
import type { SubmapSummary } from "@/lib/types"

export interface SubmapNodeData {
  submap: SubmapSummary
}

// Folder-style node for a child map: colored by the WORST status in its whole
// subtree (Doc 4 §4.6). Double-click drills in (handled on the canvas).
export const SubmapNode = memo(function SubmapNode({ data }: NodeProps<SubmapNodeData>) {
  const sm = data.submap
  const worst = worstStatus(sm)
  const st = statusStyle(worst, null)
  const total = sm.up + sm.down + sm.orphaned + sm.unknown

  return (
    <div
      title={`${sm.name}: ${sm.up} up · ${sm.down} down · ${sm.orphaned} orphaned — double-click to open`}
      className={`relative flex w-40 flex-col items-center gap-1 rounded-md border-2 bg-muted/40 p-2 shadow-sm ${st.border}`}
    >
      <span className={`absolute left-1.5 top-1.5 h-2 w-2 rounded-full ${st.dot}`} />
      <Folder className="h-8 w-8" />
      <span className="max-w-full truncate text-xs font-semibold">{sm.name}</span>
      <span className="text-[10px] text-muted-foreground">
        {total === 0 ? "empty" : `▲ ${sm.up} · ▼ ${sm.down}${sm.orphaned ? ` · ⚠ ${sm.orphaned}` : ""}`}
      </span>
    </div>
  )
})
