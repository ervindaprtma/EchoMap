import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useNavigate, useParams } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type Node,
} from "reactflow"
import "reactflow/dist/style.css"

import { api } from "@/lib/api"
import { useRole } from "@/lib/auth"
import type { Device, IconRow, MapRow, TopologyResponse } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { DeviceFormDialog } from "@/components/devices/DeviceFormDialog"
import { DeviceNode, type DeviceNodeData } from "@/components/topology/DeviceNode"
import { MapBreadcrumb } from "@/components/topology/MapBreadcrumb"
import { SubmapNode, type SubmapNodeData } from "@/components/topology/SubmapNode"

const nodeTypes = { device: DeviceNode, submap: SubmapNode }

// Grid seed for nodes without saved coordinates — never overwrites a saved
// layout (Doc 4 §4.2); positions persist only via the explicit Save Layout.
const seed = (i: number) => ({ x: 40 + (i % 6) * 190, y: 40 + Math.floor(i / 6) * 130 })

export default function Topology() {
  const { mapId: mapIdParam } = useParams()
  const mapId = mapIdParam ? Number(mapIdParam) : null
  const navigate = useNavigate()
  const qc = useQueryClient()

  const topo = useQuery({
    queryKey: ["topology", mapId ?? 0],
    queryFn: () => api<TopologyResponse>(`/api/v1/topology${mapId ? `?map_id=${mapId}` : ""}`),
  })
  const maps = useQuery({
    queryKey: ["maps"],
    queryFn: () => api<{ items: MapRow[] }>("/api/v1/maps"),
  })
  const icons = useQuery({
    queryKey: ["icons"],
    queryFn: () => api<{ items: IconRow[] }>("/api/v1/icons"),
  })
  const { isAdmin } = useRole() // Operators view topology read-only; the API enforces it
  const channels = useQuery({
    queryKey: ["settings", "channels"],
    queryFn: () => api<{ webssh_url: string }>("/api/v1/settings/channels"),
    enabled: isAdmin, // settings is Admin-only; only launchers (Admin) use webssh_url
  })

  const [editMode, setEditMode] = useState(false)
  const [editing, setEditing] = useState<Device | null>(null)
  const dirty = useRef(new Map<number, { x: number; y: number }>())
  const [dirtyCount, setDirtyCount] = useState(0)

  const [nodes, setNodes, onNodesChange] = useNodesState([])
  const [edges, setEdges, onEdgesChange] = useEdgesState([])

  const websshBase = channels.data?.webssh_url ?? ""
  const onEditNode = useCallback((d: Device) => setEditing(d), [])

  // Rebuild the canvas from server data (statuses arrive patched via realtime).
  useEffect(() => {
    if (!topo.data) return
    const iconById = new Map((icons.data?.items ?? []).map((i) => [i.id, i.data_url]))
    let unplaced = 0

    const deviceNodes: Node<DeviceNodeData>[] = topo.data.nodes.map((d) => {
      // Unsaved drags survive rebuilds: a WS status event mid-edit invalidates
      // this query, and without the overlay the refetch would snap dragged
      // nodes back to their server positions before "Save Layout".
      const draft = dirty.current.get(d.id)
      return {
        id: `d-${d.id}`,
        type: "device",
        position: draft
          ? { x: draft.x, y: draft.y }
          : d.pos_x != null && d.pos_y != null
            ? { x: d.pos_x, y: d.pos_y }
            : seed(unplaced++),
        data: {
          device: d,
          iconUrl: d.icon_id != null ? iconById.get(d.icon_id) : undefined,
          websshBase,
          onEdit: onEditNode,
        },
      }
    })
    const submapNodes: Node<SubmapNodeData>[] = topo.data.submaps.map((sm) => ({
      id: `m-${sm.map_id}`,
      type: "submap",
      position:
        sm.pos_x != null && sm.pos_y != null ? { x: sm.pos_x, y: sm.pos_y } : seed(unplaced++),
      data: { submap: sm },
    }))
    setNodes([...deviceNodes, ...submapNodes])

    setEdges(
      topo.data.edges.map((e): Edge => ({
        id: `e-${e.id}`,
        source: `d-${e.source_device_id}`,
        target: `d-${e.target_device_id}`,
        label: e.label ?? undefined,
        style: e.link_type === "AUTO" ? { strokeDasharray: "6 3" } : undefined,
        data: { edgeId: e.id, linkType: e.link_type },
      })),
    )
  }, [topo.data, icons.data, websshBase, onEditNode, setNodes, setEdges])

  const saveLayout = useMutation({
    mutationFn: () =>
      api("/api/v1/topology/nodes/positions/bulk", {
        method: "POST",
        body: JSON.stringify({
          nodes: [...dirty.current.entries()].map(([id, p]) => ({ id, pos_x: p.x, pos_y: p.y })),
        }),
      }),
    onSuccess: () => {
      dirty.current.clear()
      setDirtyCount(0)
      qc.invalidateQueries({ queryKey: ["topology"] })
    },
  })

  const addEdge = useMutation({
    mutationFn: (c: { source: number; target: number }) =>
      api("/api/v1/topology/edges", {
        method: "POST",
        body: JSON.stringify({ source_device_id: c.source, target_device_id: c.target }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["topology"] }),
  })

  const removeEdge = useMutation({
    mutationFn: (edgeId: number) => api(`/api/v1/topology/edges/${edgeId}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["topology"] }),
  })

  const onNodeDragStop = useCallback(
    (_: unknown, node: Node) => {
      if (node.id.startsWith("d-")) {
        dirty.current.set(Number(node.id.slice(2)), node.position)
        setDirtyCount(dirty.current.size)
      } else if (node.id.startsWith("m-")) {
        // Submap nodes save immediately — they're rare, one PATCH is fine.
        api(`/api/v1/maps/${node.id.slice(2)}`, {
          method: "PATCH",
          body: JSON.stringify({ pos_x: node.position.x, pos_y: node.position.y }),
        }).catch(() => qc.invalidateQueries({ queryKey: ["topology"] }))
      }
    },
    [qc],
  )

  const onConnect = useCallback(
    (c: Connection) => {
      if (c.source?.startsWith("d-") && c.target?.startsWith("d-")) {
        addEdge.mutate({ source: Number(c.source.slice(2)), target: Number(c.target.slice(2)) })
      }
    },
    [addEdge],
  )

  const onEdgeClick = useCallback(
    (_: unknown, edge: Edge) => {
      if (!editMode) return
      const { edgeId, linkType } = (edge.data ?? {}) as { edgeId?: number; linkType?: string }
      if (linkType !== "MANUAL") return // AUTO edges are derived; not hand-deletable
      if (edgeId && window.confirm("Delete this manual link?")) removeEdge.mutate(edgeId)
    },
    [editMode, removeEdge],
  )

  const onNodeDoubleClick = useCallback(
    (_: unknown, node: Node) => {
      if (node.id.startsWith("m-")) navigate(`/topology/${node.id.slice(2)}`)
      else if (node.id.startsWith("d-")) setEditing((node.data as DeviceNodeData).device)
    },
    [navigate],
  )

  const newSubmap = useMutation({
    mutationFn: (name: string) =>
      api("/api/v1/maps", {
        method: "POST",
        body: JSON.stringify({ name, parent_map_id: mapId }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["maps"] })
      qc.invalidateQueries({ queryKey: ["topology"] })
    },
  })

  const canvasEmpty = useMemo(
    () => (topo.data?.nodes.length ?? 0) === 0 && (topo.data?.submaps.length ?? 0) === 0,
    [topo.data],
  )

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <MapBreadcrumb maps={maps.data?.items ?? []} mapId={mapId} />
        <div className="flex items-center gap-3">
          {editMode && (
            <>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  const name = window.prompt("Submap name?")
                  if (name?.trim()) newSubmap.mutate(name.trim())
                }}
              >
                New Submap
              </Button>
              <Button size="sm" disabled={dirtyCount === 0 || saveLayout.isPending} onClick={() => saveLayout.mutate()}>
                Save Layout{dirtyCount > 0 ? ` (${dirtyCount})` : ""}
              </Button>
            </>
          )}
          {isAdmin && (
            <div className="flex items-center gap-2">
              <Switch id="edit-mode" checked={editMode} onCheckedChange={setEditMode} />
              <Label htmlFor="edit-mode" className="text-sm">Edit Mode</Label>
            </div>
          )}
        </div>
      </div>

      <div className="h-[calc(100vh-11rem)] rounded-md border">
        {topo.isError ? (
          <p className="p-6 text-center text-sm text-red-500">Failed to load topology</p>
        ) : canvasEmpty && !topo.isLoading ? (
          <p className="p-6 text-center text-sm text-muted-foreground">
            Nothing on this map yet — add devices to it from the Devices page, or create a submap in Edit Mode.
          </p>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            nodesDraggable={editMode}
            nodesConnectable={editMode}
            elementsSelectable
            onNodeDragStop={onNodeDragStop}
            onConnect={onConnect}
            onEdgeClick={onEdgeClick}
            onNodeDoubleClick={onNodeDoubleClick}
            fitView
            proOptions={{ hideAttribution: true }}
          >
            <Background />
            <MiniMap pannable zoomable />
            <Controls showInteractive={false} />
          </ReactFlow>
        )}
      </div>

      {(saveLayout.isError || addEdge.isError || removeEdge.isError || newSubmap.isError) && (
        <p className="text-xs text-red-500">
          {((saveLayout.error ?? addEdge.error ?? removeEdge.error ?? newSubmap.error) as Error).message}
        </p>
      )}
      <p className="text-xs text-muted-foreground">
        Double-click a device to edit it, a folder to open the submap.
        {editMode && " Drag between node handles to create a manual link; click a solid link to delete it."}
      </p>

      <DeviceFormDialog open={editing !== null} onOpenChange={(o) => !o && setEditing(null)} device={editing} />
    </div>
  )
}
