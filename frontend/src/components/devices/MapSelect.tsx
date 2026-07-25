import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import type { MapRow } from "@/lib/types"

// Flatten the maps tree into indented options (Root map = value "0").
export function mapOptions(maps: MapRow[]): { id: number; label: string }[] {
  const byParent = new Map<number | null, MapRow[]>()
  for (const m of maps) {
    const kids = byParent.get(m.parent_map_id) ?? []
    kids.push(m)
    byParent.set(m.parent_map_id, kids)
  }
  const out: { id: number; label: string }[] = []
  const walk = (parent: number | null, depth: number, seen: Set<number>) => {
    for (const m of byParent.get(parent) ?? []) {
      if (seen.has(m.id)) continue // corrupt cycle guard
      seen.add(m.id)
      out.push({ id: m.id, label: `${"  ".repeat(depth)}${m.name}` })
      walk(m.id, depth + 1, seen)
    }
  }
  walk(null, 0, new Set())
  return out
}

export function MapSelect({
  maps, value, onChange,
}: {
  maps: MapRow[]
  value: number | null
  onChange: (id: number | null) => void
}) {
  return (
    <Select value={String(value ?? 0)} onValueChange={(v) => onChange(v === "0" ? null : Number(v))}>
      <SelectTrigger>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="0">Root map</SelectItem>
        {mapOptions(maps).map((o) => (
          <SelectItem key={o.id} value={String(o.id)}>{o.label}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
