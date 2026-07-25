import { describe, expect, it } from "vitest"
import { handoffCommand, handoffUri, serviceAction, websshUrl } from "./serviceLaunch"
import type { Device, DeviceService } from "./types"

const device = { ip_address: "10.0.0.5", hostname: "fw.example.com" } as Device
const svc = (over: Partial<DeviceService>): DeviceService =>
  ({ id: 1, device_id: 1, label: "x", kind: "HTTP", port: null, path: null, url: null, ...over }) as DeviceService

describe("serviceAction", () => {
  it("HTTP omits the default port, includes path", () => {
    expect(serviceAction(svc({ kind: "HTTP", path: "/admin" }), device))
      .toEqual({ kind: "open", url: "http://10.0.0.5/admin" })
  })
  it("HTTPS custom port is included", () => {
    expect(serviceAction(svc({ kind: "HTTPS", port: 8443 }), device))
      .toEqual({ kind: "open", url: "https://10.0.0.5:8443" })
  })
  it("CUSTOM_URL substitutes {ip} and {hostname}", () => {
    expect(serviceAction(svc({ kind: "CUSTOM_URL", url: "https://wiki/x?h={hostname}&i={ip}" }), device))
      .toEqual({ kind: "open", url: "https://wiki/x?h=fw.example.com&i=10.0.0.5" })
  })
  it("CUSTOM_URL {hostname} falls back to IP when device has none", () => {
    const noHost = { ...device, hostname: null } as Device
    expect(serviceAction(svc({ kind: "CUSTOM_URL", url: "http://{hostname}/" }), noHost))
      .toEqual({ kind: "open", url: "http://10.0.0.5/" })
  })
  it("SSH defaults to 22 and produces handoff pieces", () => {
    const a = serviceAction(svc({ kind: "SSH" }), device)
    expect(a).toEqual({ kind: "handoff", proto: "ssh", host: "10.0.0.5", port: 22 })
    if (a.kind === "handoff") {
      expect(handoffUri(a)).toBe("ssh://10.0.0.5:22")
      expect(handoffCommand(a)).toBe("ssh 10.0.0.5 -p 22")
      expect(websshUrl("https://gw/term?theme=dark", a)).toBe("https://gw/term?theme=dark&host=10.0.0.5&port=22")
      expect(websshUrl("https://gw/term", a)).toBe("https://gw/term?host=10.0.0.5&port=22")
    }
  })
})
