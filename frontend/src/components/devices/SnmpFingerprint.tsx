import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ScanLine } from "lucide-react"
import { api } from "@/lib/api"
import type { Device } from "@/lib/types"
import { Button } from "@/components/ui/button"

// Slice 4: SNMP fingerprint panel inside the device dialog (edit mode). Shows the
// detected vendor/model + discovered interfaces and lets an Admin re-run the
// fingerprint on demand. The read community lives in Settings → SNMP.
interface Interface {
  if_index: number | null
  if_name: string | null
  mac_address: string | null
  ip_address: string | null
}

export function SnmpFingerprint({ device }: { device: Device }) {
  const qc = useQueryClient()
  const interfaces = useQuery({
    queryKey: ["interfaces", device.id],
    queryFn: () => api<{ items: Interface[] }>(`/api/v1/devices/${device.id}/interfaces`),
  })

  const run = useMutation({
    mutationFn: () =>
      api<{ vendor: string; model: string; sys_name: string; interfaces: unknown[] }>(
        `/api/v1/devices/${device.id}/snmp/fingerprint`,
        { method: "POST" },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["interfaces", device.id] })
      qc.invalidateQueries({ queryKey: ["devices"] })
    },
  })

  const ifaces = interfaces.data?.items ?? []

  return (
    <div className="space-y-3 rounded-md border p-3">
      <div className="flex items-center justify-between">
        <div className="text-sm">
          <span className="text-muted-foreground">Detected: </span>
          {device.vendor || device.model ? (
            <span className="font-medium">{[device.vendor, device.model].filter(Boolean).join(" ")}</span>
          ) : (
            <span className="text-muted-foreground">not fingerprinted yet</span>
          )}
        </div>
        <Button size="sm" variant="outline" disabled={run.isPending} onClick={() => run.mutate()}>
          <ScanLine className="mr-1 h-4 w-4" />
          {run.isPending ? "Scanning…" : "Fingerprint now"}
        </Button>
      </div>

      {run.isError && <p className="text-sm text-red-500">{(run.error as Error).message}</p>}

      {ifaces.length > 0 && (
        <div className="max-h-40 overflow-y-auto text-xs">
          <div className="mb-1 text-muted-foreground">{ifaces.length} interface(s)</div>
          <table className="w-full">
            <tbody>
              {ifaces.map((i) => (
                <tr key={i.if_index} className="border-t">
                  <td className="py-1 pr-2 font-mono">{i.if_index}</td>
                  <td className="py-1 pr-2">{i.if_name ?? "—"}</td>
                  <td className="py-1 font-mono text-muted-foreground">{i.mac_address ?? ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
