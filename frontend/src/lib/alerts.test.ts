import { describe, expect, it } from "vitest"
import { alertKind } from "./alerts"

describe("alertKind", () => {
  it("announces a device that failed its own probe", () => {
    expect(alertKind({ to_status: "DOWN", from_status: "UP", down_reason: "PROBE" })).toBe("down")
  })

  it("stays silent for cascade children — the parent alert covers them", () => {
    expect(alertKind({ to_status: "DOWN", from_status: "UP", down_reason: "PARENT" })).toBeNull()
  })

  it("announces recovery from a real DOWN", () => {
    expect(alertKind({ to_status: "UP", from_status: "DOWN" })).toBe("up")
  })

  it("stays silent when a cascade victim is released to UNKNOWN and then probes UP", () => {
    expect(alertKind({ to_status: "UNKNOWN", from_status: "DOWN", down_reason: null })).toBeNull()
    expect(alertKind({ to_status: "UP", from_status: "UNKNOWN" })).toBeNull()
  })

  it("stays silent for a newly added device's first UP", () => {
    expect(alertKind({ to_status: "UP", from_status: "UNKNOWN" })).toBeNull()
  })
})
