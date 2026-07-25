import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Globe, Lock, Pencil, Search, TerminalSquare, Waypoints } from "lucide-react"
import { api } from "@/lib/api"
import { useRole } from "@/lib/auth"
import {
  ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel,
  ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { serviceAction, type LaunchAction } from "@/lib/serviceLaunch"
import type { Device, DeviceService } from "@/lib/types"
import { SshHandoffDialog } from "./SshHandoffDialog"
import { ToolsPanel } from "./ToolsPanel"

type Handoff = Extract<LaunchAction, { kind: "handoff" }>
type Tool = "ping" | "traceroute" | "dns-lookup"

// Right-click menu for a device node (Doc 4 §4.5): configured service launchers
// + on-demand tools + edit. Wraps the node so the whole card is the trigger.
export function NodeContextMenu({
  device, websshBase, onEdit, children,
}: {
  device: Device
  websshBase: string
  onEdit: (d: Device) => void
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [handoff, setHandoff] = useState<{ action: Handoff; label: string } | null>(null)
  const [tool, setTool] = useState<Tool | null>(null)
  // Service launchers + device edit are Admin+ (PRD P11); Operators keep Tools.
  const { isAdmin } = useRole()

  // Services load lazily, only once the menu is opened (and only for Admins,
  // whose GET /services the API allows — Operators would 403).
  const services = useQuery({
    queryKey: ["services", device.id],
    queryFn: () => api<{ items: DeviceService[] }>(`/api/v1/devices/${device.id}/services`),
    enabled: open && isAdmin,
  })

  const launch = (svc: DeviceService) => {
    const action = serviceAction(svc, device)
    if (action.kind === "open") {
      window.open(action.url, "_blank", "noopener,noreferrer")
    } else {
      setHandoff({ action, label: svc.label })
    }
  }

  const items = services.data?.items ?? []

  return (
    <>
      <ContextMenu onOpenChange={setOpen}>
        <ContextMenuTrigger>{children}</ContextMenuTrigger>
        <ContextMenuContent className="w-56">
          <ContextMenuLabel className="truncate">
            {device.name} <span className="font-mono text-xs text-muted-foreground">{device.ip_address}</span>
          </ContextMenuLabel>
          <ContextMenuSeparator />

          {isAdmin && (
            <>
              <ContextMenuLabel className="text-xs text-muted-foreground">Services</ContextMenuLabel>
              {open && services.isLoading && (
                <ContextMenuItem disabled>Loading…</ContextMenuItem>
              )}
              {open && !services.isLoading && items.length === 0 && (
                <ContextMenuItem disabled>No services configured</ContextMenuItem>
              )}
              {items.map((svc) => {
                const isUrl = svc.kind === "HTTP" || svc.kind === "HTTPS" || svc.kind === "CUSTOM_URL"
                return (
                  <ContextMenuItem key={svc.id} onSelect={() => launch(svc)}>
                    {isUrl ? <Globe className="mr-2 h-4 w-4" /> : <Lock className="mr-2 h-4 w-4" />}
                    <span className="truncate">{svc.label}</span>
                    <span className="ml-auto text-xs text-muted-foreground">
                      {svc.kind}{svc.port ? `:${svc.port}` : ""}
                    </span>
                  </ContextMenuItem>
                )
              })}
              <ContextMenuSeparator />
            </>
          )}
          <ContextMenuSub>
            <ContextMenuSubTrigger>
              <TerminalSquare className="mr-2 h-4 w-4" />
              Tools
            </ContextMenuSubTrigger>
            <ContextMenuSubContent>
              <ContextMenuItem onSelect={() => setTool("ping")}>
                <Waypoints className="mr-2 h-4 w-4" />
                Ping
              </ContextMenuItem>
              <ContextMenuItem onSelect={() => setTool("traceroute")}>
                <Waypoints className="mr-2 h-4 w-4" />
                Traceroute
              </ContextMenuItem>
              <ContextMenuItem onSelect={() => setTool("dns-lookup")}>
                <Search className="mr-2 h-4 w-4" />
                DNS Lookup
              </ContextMenuItem>
            </ContextMenuSubContent>
          </ContextMenuSub>

          {isAdmin && (
            <>
              <ContextMenuSeparator />
              <ContextMenuItem onSelect={() => onEdit(device)}>
                <Pencil className="mr-2 h-4 w-4" />
                Edit Device
              </ContextMenuItem>
            </>
          )}
        </ContextMenuContent>
      </ContextMenu>

      <SshHandoffDialog
        open={handoff !== null}
        onOpenChange={(o) => !o && setHandoff(null)}
        action={handoff?.action ?? null}
        label={handoff?.label ?? ""}
        websshBase={websshBase}
      />
      <ToolsPanel device={tool ? device : null} tool={tool} onClose={() => setTool(null)} />
    </>
  )
}
