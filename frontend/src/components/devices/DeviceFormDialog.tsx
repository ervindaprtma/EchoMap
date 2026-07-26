import { useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { descendantIds } from "@/lib/descendants"
import type { Device, DeviceList, IconRow, MapRow } from "@/lib/types"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { IconPicker } from "./IconPicker"
import { MapSelect } from "./MapSelect"
import { ParentSelect } from "./ParentSelect"
import { ServicesEditor } from "./ServicesEditor"
import { MonitorsEditor } from "./MonitorsEditor"
import { SubnetScan } from "./SubnetScan"
import { SnmpFingerprint } from "./SnmpFingerprint"

// Create + edit dialog (Doc 4 §3.2). Controlled inputs + server-side validation:
// the API's error strings (cycle, duplicate, unresolvable hostname) surface
// inline. Skipped react-hook-form/zod — one form doesn't earn the dependency.
export function DeviceFormDialog({
  open, onOpenChange, device,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  device?: Device | null
}) {
  const qc = useQueryClient()
  const editing = !!device

  const [tab, setTab] = useState("general")
  const [mode, setMode] = useState<"ip" | "hostname">("ip")
  const [target, setTarget] = useState("")
  const [name, setName] = useState("")
  const [parentId, setParentId] = useState<number | null>(null)
  const [mapId, setMapId] = useState<number | null>(null)
  const [iconId, setIconId] = useState<number | null>(null)
  const [snmp, setSnmp] = useState(false)

  useEffect(() => {
    if (!open) return
    setTab("general")
    setMode(device?.hostname ? "hostname" : "ip")
    setTarget(device ? (device.hostname ?? device.ip_address) : "")
    setName(device?.name ?? "")
    setParentId(device?.parent_device_id ?? null)
    setMapId(device?.map_id ?? null)
    setIconId(device?.icon_id ?? null)
    setSnmp(device?.snmp_enabled ?? false)
  }, [open, device])

  // Pickers' data, fetched only while the dialog is open.
  // ponytail: parent options read one 200-row page; paginate when fleets outgrow it.
  const allDevices = useQuery({
    queryKey: ["devices", "all"],
    queryFn: () => api<DeviceList>("/api/v1/devices?page_size=200"),
    enabled: open,
  })
  const maps = useQuery({
    queryKey: ["maps"],
    queryFn: () => api<{ items: MapRow[] }>("/api/v1/maps"),
    enabled: open,
  })
  const icons = useQuery({
    queryKey: ["icons"],
    queryFn: () => api<{ items: IconRow[] }>("/api/v1/icons"),
    enabled: open,
  })

  // Exclude self + descendants so the picker can't propose a cycle (Doc 3 §5).
  const excludeIds = useMemo(() => {
    const items = allDevices.data?.items ?? []
    if (!device) return new Set<number>()
    const set = descendantIds(items, device.id)
    set.add(device.id)
    return set
  }, [allDevices.data, device])

  const save = useMutation({
    mutationFn: () => {
      if (editing) {
        const body: Record<string, unknown> = {}
        if (name.trim() !== device!.name) body.name = name.trim()
        const hostname = mode === "hostname" ? target.trim() : ""
        if (hostname !== (device!.hostname ?? "")) body.hostname = hostname // "" clears
        if (parentId !== device!.parent_device_id) body.parent_device_id = parentId ?? 0 // 0 clears
        if (mapId !== device!.map_id) body.map_id = mapId ?? 0
        if (iconId !== device!.icon_id) body.icon_id = iconId ?? 0
        if (snmp !== device!.snmp_enabled) body.snmp_enabled = snmp
        return api<Device>(`/api/v1/devices/${device!.id}`, {
          method: "PATCH",
          body: JSON.stringify(body),
        })
      }
      return api<Device>("/api/v1/devices", {
        method: "POST",
        body: JSON.stringify({
          ...(mode === "ip" ? { ip_address: target.trim() } : { hostname: target.trim() }),
          name: name.trim() || undefined,
          parent_device_id: parentId ?? undefined,
          map_id: mapId ?? undefined,
          icon_id: iconId ?? undefined,
          snmp_enabled: snmp,
        }),
      })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["devices"] })
      qc.invalidateQueries({ queryKey: ["topology"] })
      onOpenChange(false)
    },
  })

  const canSave = editing || target.trim().length > 0

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${device!.name}` : "Add Device"}</DialogTitle>
        </DialogHeader>

        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="general">{editing ? "General" : "Single Device"}</TabsTrigger>
            {editing && <TabsTrigger value="services">Services</TabsTrigger>}
            {editing && <TabsTrigger value="monitoring">Monitoring</TabsTrigger>}
            {!editing && <TabsTrigger value="subnet">Subnet Scan</TabsTrigger>}
          </TabsList>

          <TabsContent value="general" className="space-y-4 pt-2">
            {editing ? (
              <div className="space-y-2">
                <Label htmlFor="hostname">Hostname (optional — auto-resolves to IP)</Label>
                <Input
                  id="hostname"
                  value={mode === "hostname" ? target : ""}
                  placeholder="fw.branch-3.example.com"
                  onChange={(e) => { setMode("hostname"); setTarget(e.target.value) }}
                />
                <p className="text-xs text-muted-foreground">
                  Current IP: <span className="font-mono">{device!.ip_address}</span>
                  {device!.hostname ? " (kept fresh by the resolver)" : ""}
                </p>
              </div>
            ) : (
              <div className="space-y-2">
                <ToggleGroup
                  type="single"
                  value={mode}
                  onValueChange={(v) => v && setMode(v as "ip" | "hostname")}
                  className="justify-start"
                >
                  <ToggleGroupItem value="ip">IP address</ToggleGroupItem>
                  <ToggleGroupItem value="hostname">Hostname</ToggleGroupItem>
                </ToggleGroup>
                <Input
                  value={target}
                  onChange={(e) => setTarget(e.target.value)}
                  placeholder={mode === "ip" ? "10.10.20.5" : "fw.branch-3.example.com (resolved on save)"}
                  autoFocus
                />
              </div>
            )}

            <div className="space-y-2">
              <Label htmlFor="dev-name">Display name (optional)</Label>
              <Input id="dev-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="core-sw-01" />
            </div>

            <div className="space-y-2">
              <Label>Dependency parent — if it goes down, this device is marked down too</Label>
              <ParentSelect
                devices={allDevices.data?.items ?? []}
                excludeIds={excludeIds}
                value={parentId}
                onChange={setParentId}
              />
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-2">
                <Label>Map</Label>
                <MapSelect maps={maps.data?.items ?? []} value={mapId} onChange={setMapId} />
              </div>
              <div className="space-y-2">
                <Label>Icon</Label>
                <IconPicker icons={icons.data?.items ?? []} value={iconId} onChange={setIconId} />
              </div>
            </div>

            <div className="flex items-center gap-2">
              <Switch id="snmp" checked={snmp} onCheckedChange={setSnmp} />
              <Label htmlFor="snmp">Enable SNMP fingerprinting</Label>
            </div>

            {editing && snmp && <SnmpFingerprint device={device!} />}
          </TabsContent>

          {editing && (
            <TabsContent value="services" className="pt-2">
              <ServicesEditor deviceId={device!.id} />
            </TabsContent>
          )}

          {editing && (
            <TabsContent value="monitoring" className="pt-2">
              <MonitorsEditor deviceId={device!.id} />
            </TabsContent>
          )}

          {!editing && (
            <TabsContent value="subnet" className="pt-2">
              <SubnetScan onClose={() => onOpenChange(false)} />
            </TabsContent>
          )}
        </Tabs>

        {save.isError && <p className="text-sm text-red-500">{(save.error as Error).message}</p>}

        {/* Subnet Scan carries its own Scan/Done buttons — hide the single-device footer there. */}
        {tab !== "subnet" && (
          <DialogFooter>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button disabled={!canSave || save.isPending} onClick={() => save.mutate()}>
              {save.isPending ? "Saving…" : editing ? "Save changes" : "Add Device"}
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  )
}
