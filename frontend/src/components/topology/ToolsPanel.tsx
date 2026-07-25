import { useEffect, useRef } from "react"
import { useMutation } from "@tanstack/react-query"
import { api } from "@/lib/api"
import {
  Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle,
} from "@/components/ui/sheet"
import type { Device } from "@/lib/types"

type Tool = "ping" | "traceroute" | "dns-lookup"

interface Hop {
  ttl: number
  ip: string | null
  hostname: string | null
  rtt_ms: number | null
}

// Runs one of the on-demand tools (Doc 5 §4) against the node's target and
// renders the result. Targets can be IP or hostname — the backend resolves both.
export function ToolsPanel({
  device, tool, onClose,
}: {
  device: Device | null
  tool: Tool | null
  onClose: () => void
}) {
  const open = !!device && !!tool
  const target = device?.hostname ?? device?.ip_address ?? ""

  const run = useMutation({
    mutationFn: () => {
      if (tool === "dns-lookup") {
        return api<unknown>("/api/v1/tools/dns-lookup", { method: "POST", body: JSON.stringify({ name: target }) })
      }
      const path = tool === "ping" ? "/api/v1/tools/ping" : "/api/v1/tools/traceroute"
      return api<unknown>(path, { method: "POST", body: JSON.stringify({ target }) })
    },
  })

  // Fire once per (tool, device) selection. Effect + key-guard instead of
  // mutating during render: render-phase mutations double-fire under
  // StrictMode and update state mid-render.
  const firedFor = useRef<string | null>(null)
  const mutate = run.mutate
  useEffect(() => {
    const key = open ? `${tool}:${device?.id}` : null
    if (key && firedFor.current !== key) {
      firedFor.current = key
      mutate()
    }
    if (!key) firedFor.current = null
  }, [open, tool, device?.id, mutate])

  return (
    <Sheet
      open={open}
      onOpenChange={(o) => {
        if (!o) { run.reset(); onClose() }
      }}
    >
      <SheetContent className="w-full sm:max-w-md">
        <SheetHeader>
          <SheetTitle className="capitalize">{tool?.replace("-", " ")}</SheetTitle>
          <SheetDescription>
            {device?.name} — <span className="font-mono">{target}</span>
          </SheetDescription>
        </SheetHeader>

        <div className="mt-4 text-sm">
          {run.isPending && <p className="text-muted-foreground">Running…</p>}
          {run.isError && <p className="text-red-500">{(run.error as Error).message}</p>}
          {run.isSuccess && <ToolResult tool={tool!} data={run.data} />}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function ToolResult({ tool, data }: { tool: Tool; data: unknown }) {
  if (tool === "ping") {
    const d = data as { alive: boolean; rtt_ms?: number }
    return d.alive ? (
      <p className="text-green-500">Alive — {d.rtt_ms?.toFixed(2)} ms</p>
    ) : (
      <p className="text-red-500">No reply (timeout)</p>
    )
  }
  if (tool === "traceroute") {
    const d = data as { target_ip: string; hops: Hop[] }
    return (
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">to {d.target_ip}</p>
        <table className="w-full font-mono text-xs">
          <tbody>
            {d.hops.map((h) => (
              <tr key={h.ttl} className="border-b last:border-0">
                <td className="py-1 pr-2 text-muted-foreground">{h.ttl}</td>
                <td className="py-1 pr-2">{h.ip ?? "*"}</td>
                <td className="py-1 pr-2 text-muted-foreground">{h.hostname ?? ""}</td>
                <td className="py-1 text-right">{h.rtt_ms != null ? `${h.rtt_ms.toFixed(1)} ms` : ""}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  const d = data as { ips: string[]; cname?: string; ptr?: string[] }
  return (
    <div className="space-y-2 font-mono text-xs">
      {d.cname && <p><span className="text-muted-foreground">CNAME </span>{d.cname}</p>}
      {d.ips?.map((ip) => <p key={ip}>{ip}</p>)}
      {d.ptr?.map((p) => <p key={p}><span className="text-muted-foreground">PTR </span>{p}</p>)}
    </div>
  )
}
