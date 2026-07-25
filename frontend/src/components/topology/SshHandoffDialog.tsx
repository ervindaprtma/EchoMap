import { useState } from "react"
import { Copy, ExternalLink, TerminalSquare } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { handoffCommand, handoffUri, websshUrl, type LaunchAction } from "@/lib/serviceLaunch"

type Handoff = Extract<LaunchAction, { kind: "handoff" }>

// SSH/Telnet launcher. Three zero-credential paths — EchoMap never asks for,
// stores, or proxies credentials; the user authenticates in their own client.
export function SshHandoffDialog({
  open, onOpenChange, action, label, websshBase,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  action: Handoff | null
  label: string
  websshBase: string // "" when no gateway is configured
}) {
  const [copied, setCopied] = useState(false)
  if (!action) return null
  const cmd = handoffCommand(action)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(cmd)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard blocked (insecure origin) — the command is visible to copy manually */
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{label}</DialogTitle>
          <DialogDescription>
            {action.proto.toUpperCase()} to <span className="font-mono">{action.host}:{action.port}</span>.
            Credentials are never stored in EchoMap — authenticate in your own client.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Button
            variant="secondary"
            className="w-full justify-start"
            onClick={() => { window.location.href = handoffUri(action); onOpenChange(false) }}
          >
            <ExternalLink className="mr-2 h-4 w-4" />
            Open in {action.proto === "ssh" ? "SSH" : "Telnet"} app (PuTTY / OpenSSH)
          </Button>

          {websshBase && (
            <Button
              variant="secondary"
              className="w-full justify-start"
              onClick={() => { window.open(websshUrl(websshBase, action), "_blank", "noopener,noreferrer"); onOpenChange(false) }}
            >
              <TerminalSquare className="mr-2 h-4 w-4" />
              Open web terminal
            </Button>
          )}

          <div className="flex items-center gap-2 rounded border bg-muted/40 px-2 py-1.5">
            <code className="flex-1 truncate text-xs">{cmd}</code>
            <Button size="sm" variant="ghost" onClick={copy}>
              <Copy className="mr-1 h-3.5 w-3.5" />
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>Close</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
