import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Trash2 } from "lucide-react"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import type { DeviceService, ServiceKind } from "@/lib/types"

const KINDS: ServiceKind[] = ["HTTP", "HTTPS", "SSH", "TELNET", "CUSTOM_URL"]
const DEFAULT_PORT: Record<string, string> = { HTTP: "80", HTTPS: "443", SSH: "22", TELNET: "23" }

// CRUD for a device's right-click launchers. Deliberately NO credential fields
// anywhere in this component tree — the zero-credential rule is structural.
export function ServicesEditor({ deviceId }: { deviceId: number }) {
  const qc = useQueryClient()
  const key = ["services", deviceId]
  const { data } = useQuery({
    queryKey: key,
    queryFn: () => api<{ items: DeviceService[] }>(`/api/v1/devices/${deviceId}/services`),
  })

  const [label, setLabel] = useState("")
  const [kind, setKind] = useState<ServiceKind>("HTTP")
  const [port, setPort] = useState("")
  const [pathOrUrl, setPathOrUrl] = useState("")

  const add = useMutation({
    mutationFn: () =>
      api(`/api/v1/devices/${deviceId}/services`, {
        method: "POST",
        body: JSON.stringify({
          label: label.trim(),
          kind,
          port: port.trim() ? Number(port) : undefined,
          ...(kind === "CUSTOM_URL"
            ? { url: pathOrUrl.trim() }
            : pathOrUrl.trim()
              ? { path: pathOrUrl.trim() }
              : {}),
        }),
      }),
    onSuccess: () => {
      setLabel(""); setPort(""); setPathOrUrl("")
      qc.invalidateQueries({ queryKey: key })
    },
  })

  const del = useMutation({
    mutationFn: (id: number) => api(`/api/v1/services/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  const services = data?.items ?? []
  const isUrlKind = kind === "CUSTOM_URL"

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        Launchers shown in the device's right-click menu. Credentials are never stored —
        SSH/Telnet open your own client.
      </p>

      {services.length > 0 && (
        <ul className="divide-y rounded border text-sm">
          {services.map((s) => (
            <li key={s.id} className="flex items-center justify-between gap-2 px-3 py-1.5">
              <span>
                <span className="font-medium">{s.label}</span>{" "}
                <span className="text-xs text-muted-foreground">
                  {s.kind}
                  {s.port ? ` :${s.port}` : ""}
                  {s.path ?? ""}
                  {s.url ? ` ${s.url}` : ""}
                </span>
              </span>
              <button
                type="button"
                onClick={() => del.mutate(s.id)}
                className="text-muted-foreground hover:text-red-500"
                aria-label={`Delete ${s.label}`}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="grid grid-cols-[1fr_auto_5rem_1fr_auto] items-end gap-2">
        <Input placeholder="Label (e.g. Web UI)" value={label} onChange={(e) => setLabel(e.target.value)} />
        <Select value={kind} onValueChange={(v) => setKind(v as ServiceKind)}>
          <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
          <SelectContent>
            {KINDS.map((k) => <SelectItem key={k} value={k}>{k}</SelectItem>)}
          </SelectContent>
        </Select>
        <Input
          placeholder={DEFAULT_PORT[kind] ?? "port"}
          value={port}
          onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
          disabled={isUrlKind}
        />
        <Input
          placeholder={isUrlKind ? "https://… ({ip} / {hostname} ok)" : "/path (optional)"}
          value={pathOrUrl}
          onChange={(e) => setPathOrUrl(e.target.value)}
        />
        <Button
          type="button"
          size="sm"
          disabled={!label.trim() || (isUrlKind && !pathOrUrl.trim()) || add.isPending}
          onClick={() => add.mutate()}
        >
          Add
        </Button>
      </div>
      {add.isError && <p className="text-xs text-red-500">{(add.error as Error).message}</p>}
    </div>
  )
}
