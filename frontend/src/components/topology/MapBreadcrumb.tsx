import { Link } from "react-router-dom"
import { ChevronRight } from "lucide-react"
import type { MapRow } from "@/lib/types"

// Root / DC / Rack-1 — computed by walking parent_map_id up from the current map.
export function MapBreadcrumb({ maps, mapId }: { maps: MapRow[]; mapId: number | null }) {
  const byId = new Map(maps.map((m) => [m.id, m]))
  const path: MapRow[] = []
  let cur = mapId != null ? byId.get(mapId) : undefined
  const guard = new Set<number>()
  while (cur && !guard.has(cur.id)) {
    guard.add(cur.id)
    path.unshift(cur)
    cur = cur.parent_map_id != null ? byId.get(cur.parent_map_id) : undefined
  }

  return (
    <nav className="flex items-center gap-1 text-sm">
      <Link to="/topology" className={path.length ? "text-muted-foreground hover:text-foreground" : "font-medium"}>
        Root
      </Link>
      {path.map((m, i) => (
        <span key={m.id} className="flex items-center gap-1">
          <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
          {i === path.length - 1 ? (
            <span className="font-medium">{m.name}</span>
          ) : (
            <Link to={`/topology/${m.id}`} className="text-muted-foreground hover:text-foreground">
              {m.name}
            </Link>
          )}
        </span>
      ))}
    </nav>
  )
}
