import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useRole } from "@/lib/auth"
import type { Device, DeviceList, DeviceStatus } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { DeviceFormDialog } from "@/components/devices/DeviceFormDialog"

const STATUS_STYLES: Record<DeviceStatus, string> = {
  UP: "bg-green-500/15 text-green-500",
  DOWN: "bg-red-500/15 text-red-500",
  ORPHANED: "bg-amber-500/15 text-amber-500",
  UNKNOWN: "bg-muted text-muted-foreground",
}

function StatusBadge({ device }: { device: Device }) {
  const viaParent = device.status === "DOWN" && device.down_reason === "PARENT"
  return (
    <span
      title={viaParent ? "Down via dependency parent" : undefined}
      className={`inline-flex rounded px-2 py-0.5 text-xs font-medium ${STATUS_STYLES[device.status]} ${viaParent ? "opacity-75" : ""}`}
    >
      {device.status}
      {viaParent && " ⛓"}
    </span>
  )
}

export default function Devices() {
  const qc = useQueryClient()
  const { isAdmin } = useRole() // Operators get read-only devices (API enforces; this trims the UI)
  const { data, isLoading, isError } = useQuery({
    queryKey: ["devices"],
    queryFn: () => api<DeviceList>("/api/v1/devices"),
  })

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Device | null>(null)

  const del = useMutation({
    mutationFn: (id: number) => api(`/api/v1/devices/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["devices"] }),
  })

  const openCreate = () => { setEditing(null); setFormOpen(true) }
  const openEdit = (d: Device) => { setEditing(d); setFormOpen(true) }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">
          Devices{" "}
          {data && <span className="font-normal text-muted-foreground">({data.total})</span>}
        </h2>
        {isAdmin && <Button onClick={openCreate}>Add Device</Button>}
      </div>

      <div className="overflow-x-auto rounded-md border">
        <table className="w-full text-sm">
          <thead className="border-b bg-muted/50 text-left text-xs text-muted-foreground">
            <tr>
              <th className="px-3 py-2 font-medium">Status</th>
              <th className="px-3 py-2 font-medium">Name</th>
              <th className="px-3 py-2 font-medium">IP / Hostname</th>
              <th className="px-3 py-2 font-medium">Vendor / Model</th>
              <th className="px-3 py-2 font-medium">Site</th>
              <th className="px-3 py-2 font-medium">Source</th>
              <th className="px-3 py-2 font-medium">Last seen</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td colSpan={8} className="px-3 py-6 text-center text-muted-foreground">
                  Loading…
                </td>
              </tr>
            )}
            {isError && (
              <tr>
                <td colSpan={8} className="px-3 py-6 text-center text-red-500">
                  Failed to load devices
                </td>
              </tr>
            )}
            {data?.items.length === 0 && (
              <tr>
                <td colSpan={8} className="px-3 py-6 text-center text-muted-foreground">
                  No devices yet — click "Add Device".
                </td>
              </tr>
            )}
            {data?.items.map((d) => (
              <tr key={d.id} className="border-b last:border-0 hover:bg-muted/30">
                <td className="px-3 py-2">
                  <StatusBadge device={d} />
                </td>
                <td className="px-3 py-2 font-medium">{d.name}</td>
                <td className="px-3 py-2 font-mono text-xs">
                  {d.ip_address}
                  {d.hostname && (
                    <span className="block text-muted-foreground">{d.hostname}</span>
                  )}
                </td>
                <td className="px-3 py-2">
                  {d.vendor ? `${d.vendor} ${d.model ?? ""}`.trim() : <span className="text-muted-foreground">—</span>}
                </td>
                <td className="px-3 py-2">{d.site ?? <span className="text-muted-foreground">—</span>}</td>
                <td className="px-3 py-2 text-xs">{d.source_type}</td>
                <td className="px-3 py-2 text-xs text-muted-foreground">
                  {d.last_seen_at ? new Date(d.last_seen_at).toLocaleString() : "never"}
                </td>
                <td className="px-3 py-2 text-right text-xs">
                  {isAdmin ? (
                    <>
                      <button onClick={() => openEdit(d)} className="mr-3 text-muted-foreground hover:text-foreground">
                        Edit
                      </button>
                      <button
                        onClick={() => {
                          // ponytail: window.confirm; migrate to AlertDialog with the PRD §6 debt batch
                          if (window.confirm(`Delete ${d.name} (${d.ip_address})? This also removes its links and services.`)) {
                            del.mutate(d.id)
                          }
                        }}
                        className="text-muted-foreground hover:text-red-500"
                      >
                        Delete
                      </button>
                    </>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <DeviceFormDialog open={formOpen} onOpenChange={setFormOpen} device={editing} />
    </div>
  )
}
