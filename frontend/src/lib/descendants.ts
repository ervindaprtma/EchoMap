// descendantIds returns every device in rootId's dependency subtree.
// The device form filters these (plus rootId itself) out of the parent picker
// so the UI can't offer a choice the server would reject as a cycle (400 stays
// the authoritative backstop). Visited-set guard: bad data can't loop us.
export function descendantIds(
  devices: { id: number; parent_device_id: number | null }[],
  rootId: number,
): Set<number> {
  const byParent = new Map<number, number[]>()
  for (const d of devices) {
    if (d.parent_device_id == null) continue
    const kids = byParent.get(d.parent_device_id) ?? []
    kids.push(d.id)
    byParent.set(d.parent_device_id, kids)
  }

  const out = new Set<number>()
  const stack = [rootId]
  while (stack.length > 0) {
    const id = stack.pop()!
    for (const child of byParent.get(id) ?? []) {
      if (!out.has(child)) {
        out.add(child)
        stack.push(child)
      }
    }
  }
  out.delete(rootId) // contract: never includes the root, even in corrupt cyclic data
  return out
}
