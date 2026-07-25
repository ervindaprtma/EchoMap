import type { Device, DeviceService } from "./types"

// What clicking a service does. URLs open a new tab; SSH/Telnet hand off to the
// user's own client (zero-credential rule: EchoMap never stores or proxies logins).
export type LaunchAction =
  | { kind: "open"; url: string }
  | { kind: "handoff"; proto: "ssh" | "telnet"; host: string; port: number }

const DEFAULT_PORT: Record<string, number> = { HTTP: 80, HTTPS: 443, SSH: 22, TELNET: 23 }

function portSuffix(port: number | null, def: number): string {
  return port != null && port !== def ? `:${port}` : ""
}

export function serviceAction(svc: DeviceService, device: Device): LaunchAction {
  const host = device.ip_address
  switch (svc.kind) {
    case "HTTP":
      return { kind: "open", url: `http://${host}${portSuffix(svc.port, 80)}${svc.path ?? ""}` }
    case "HTTPS":
      return { kind: "open", url: `https://${host}${portSuffix(svc.port, 443)}${svc.path ?? ""}` }
    case "CUSTOM_URL":
      return {
        kind: "open",
        url: (svc.url ?? "")
          .split("{ip}").join(device.ip_address)
          .split("{hostname}").join(device.hostname ?? device.ip_address),
      }
    case "SSH":
      return { kind: "handoff", proto: "ssh", host, port: svc.port ?? DEFAULT_PORT.SSH }
    case "TELNET":
      return { kind: "handoff", proto: "telnet", host, port: svc.port ?? DEFAULT_PORT.TELNET }
  }
}

// ssh://host:port — the OS asks which handler (PuTTY, OpenSSH, …).
export function handoffUri(a: Extract<LaunchAction, { kind: "handoff" }>): string {
  return `${a.proto}://${a.host}:${a.port}`
}

// Copy-paste command for a terminal.
export function handoffCommand(a: Extract<LaunchAction, { kind: "handoff" }>): string {
  return a.proto === "ssh" ? `ssh ${a.host} -p ${a.port}` : `telnet ${a.host} ${a.port}`
}

// Web-SSH gateway URL (PNETLab-style HTML5 terminal), when one is configured.
export function websshUrl(base: string, a: Extract<LaunchAction, { kind: "handoff" }>): string {
  const sep = base.includes("?") ? "&" : "?"
  return `${base}${sep}host=${encodeURIComponent(a.host)}&port=${a.port}`
}
