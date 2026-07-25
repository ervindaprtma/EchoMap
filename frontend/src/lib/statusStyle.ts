import type { DeviceStatus, DownReason } from "./types"

export interface NodeStyle {
  border: string // Tailwind classes for the node card
  dot: string // the little status dot
  viaParent: boolean // true = cascade victim: dashed border + chain glyph
}

// One source of truth for node coloring (Doc 4 §4.6): green UP, solid red DOWN,
// dashed/dimmed red for cascade victims, amber ORPHANED, gray UNKNOWN.
export function statusStyle(status: DeviceStatus, downReason: DownReason): NodeStyle {
  switch (status) {
    case "UP":
      return { border: "border-green-500", dot: "bg-green-500", viaParent: false }
    case "DOWN":
      return downReason === "PARENT"
        ? { border: "border-dashed border-red-400 bg-red-500/5 opacity-80", dot: "bg-red-400", viaParent: true }
        : { border: "border-red-500 bg-red-500/10", dot: "bg-red-500", viaParent: false }
    case "ORPHANED":
      return { border: "border-amber-500 bg-amber-500/10", dot: "bg-amber-500", viaParent: false }
    default:
      return { border: "border-muted-foreground/40", dot: "bg-muted-foreground", viaParent: false }
  }
}

// worstStatus rolls a submap's subtree counts up to one color:
// any DOWN beats any ORPHANED beats UP; empty subtree reads UNKNOWN.
export function worstStatus(c: { up: number; down: number; orphaned: number; unknown: number }): DeviceStatus {
  if (c.down > 0) return "DOWN"
  if (c.orphaned > 0) return "ORPHANED"
  if (c.up > 0) return "UP"
  return "UNKNOWN"
}
