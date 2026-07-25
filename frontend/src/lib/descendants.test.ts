import { describe, expect, it } from "vitest"
import { descendantIds } from "./descendants"

const dev = (id: number, parent: number | null) => ({ id, parent_device_id: parent })

describe("descendantIds", () => {
  // core(1) <- sw(2) <- host(3); sw2(4) independent
  const devices = [dev(1, null), dev(2, 1), dev(3, 2), dev(4, null)]

  it("collects the whole subtree", () => {
    expect([...descendantIds(devices, 1)].sort()).toEqual([2, 3])
  })

  it("leaf has no descendants", () => {
    expect(descendantIds(devices, 3).size).toBe(0)
  })

  it("survives corrupt cyclic data without looping", () => {
    const cyclic = [dev(1, 2), dev(2, 1)]
    expect([...descendantIds(cyclic, 1)]).toEqual([2])
  })
})
