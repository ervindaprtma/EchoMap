import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

// Slice 3: bulk subnet discovery. POST the CIDR, then poll the batch status for a
// live progress bar. ponytail: polls the status endpoint (1.2s) rather than wiring
// the discovery.progress WS frames through realtime.ts — "a poll is live enough",
// the same call the Logs page makes. The backend still publishes the WS frames.
type BatchStatus = {
  batch_id: string
  status: "QUEUED" | "RUNNING" | "DONE"
  total: number
  processed: number
  added: number
  skipped: number
  current_ip: string
}

export function SubnetScan({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const [cidr, setCidr] = useState("")
  const [site, setSite] = useState("")
  const [snmp, setSnmp] = useState(false)
  const [batchId, setBatchId] = useState<string | null>(null)

  const scan = useMutation({
    mutationFn: () =>
      api<{ batch_id: string; total_estimated: number }>("/api/v1/devices/discover/subnet", {
        method: "POST",
        body: JSON.stringify({
          cidr: cidr.trim(),
          site: site.trim() || undefined,
          snmp_enabled: snmp,
        }),
      }),
    onSuccess: (r) => setBatchId(r.batch_id),
  })

  const status = useQuery({
    queryKey: ["discover", batchId],
    queryFn: () => api<BatchStatus>(`/api/v1/devices/discover/${batchId}/status`),
    enabled: !!batchId,
    refetchInterval: (q) => (q.state.data?.status === "DONE" ? false : 1200),
  })

  // Recolor the map/table as devices land: refetch the device caches on completion.
  const done = status.data?.status === "DONE"
  useEffect(() => {
    if (done) {
      qc.invalidateQueries({ queryKey: ["devices"] })
      qc.invalidateQueries({ queryKey: ["topology"] })
    }
  }, [done, qc])

  const running = !!batchId && !done
  const data = status.data
  const pct = data && data.total ? Math.round((data.processed / data.total) * 100) : 0

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="cidr">Subnet (CIDR)</Label>
        <Input
          id="cidr"
          value={cidr}
          onChange={(e) => setCidr(e.target.value)}
          placeholder="10.10.20.0/24"
          disabled={running}
          autoFocus
        />
        <p className="text-xs text-muted-foreground">
          Every host is pinged once; unreachable IPs are skipped, live ones added as UP devices.
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-2">
          <Label htmlFor="site">Site (optional)</Label>
          <Input id="site" value={site} onChange={(e) => setSite(e.target.value)} placeholder="DC" disabled={running} />
        </div>
        <div className="flex items-end gap-2 pb-1">
          <Switch id="scan-snmp" checked={snmp} onCheckedChange={setSnmp} disabled={running} />
          <Label htmlFor="scan-snmp">SNMP</Label>
        </div>
      </div>

      {scan.isError && <p className="text-sm text-red-500">{(scan.error as Error).message}</p>}

      {data && (
        <div className="space-y-2 rounded-md border p-3">
          <div className="flex justify-between text-sm">
            <span>{running ? "Scanning…" : "Complete"}</span>
            <span className="font-mono text-muted-foreground">
              {data.processed}/{data.total}
            </span>
          </div>
          <div className="h-2 w-full overflow-hidden rounded bg-muted">
            <div className="h-2 rounded bg-primary transition-all" style={{ width: `${pct}%` }} />
          </div>
          <div className="flex gap-4 text-xs">
            <span className="text-green-600 dark:text-green-400">added {data.added}</span>
            <span className="text-muted-foreground">skipped {data.skipped}</span>
            {running && data.current_ip && <span className="font-mono text-muted-foreground">{data.current_ip}</span>}
          </div>
        </div>
      )}

      <div className="flex justify-end gap-2">
        {done ? (
          <>
            <Button variant="ghost" onClick={() => { setBatchId(null); scan.reset() }}>
              Scan another
            </Button>
            <Button onClick={onClose}>Done</Button>
          </>
        ) : (
          <Button
            disabled={cidr.trim().length === 0 || scan.isPending || running}
            onClick={() => scan.mutate()}
          >
            {running ? "Scanning…" : "Scan subnet"}
          </Button>
        )}
      </div>
    </div>
  )
}
