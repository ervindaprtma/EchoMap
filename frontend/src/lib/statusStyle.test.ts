import { describe, expect, it } from "vitest"
import { statusStyle, worstStatus } from "./statusStyle"

describe("statusStyle", () => {
  it("distinguishes own-probe DOWN from cascade DOWN", () => {
    const probe = statusStyle("DOWN", "PROBE")
    const parent = statusStyle("DOWN", "PARENT")
    expect(probe.viaParent).toBe(false)
    expect(parent.viaParent).toBe(true)
    expect(parent.border).toContain("dashed")
    expect(probe.border).not.toContain("dashed")
  })

  it("maps the other states", () => {
    expect(statusStyle("UP", null).dot).toContain("green")
    expect(statusStyle("ORPHANED", null).dot).toContain("amber")
    expect(statusStyle("UNKNOWN", null).viaParent).toBe(false)
  })
})

describe("worstStatus", () => {
  it("DOWN beats everything", () => {
    expect(worstStatus({ up: 10, down: 1, orphaned: 5, unknown: 3 })).toBe("DOWN")
  })
  it("ORPHANED beats UP", () => {
    expect(worstStatus({ up: 10, down: 0, orphaned: 1, unknown: 0 })).toBe("ORPHANED")
  })
  it("empty subtree is UNKNOWN", () => {
    expect(worstStatus({ up: 0, down: 0, orphaned: 0, unknown: 0 })).toBe("UNKNOWN")
  })
})
