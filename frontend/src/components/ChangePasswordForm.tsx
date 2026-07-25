import { useState, type FormEvent } from "react"
import { api } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

// Shared by the forced first-login flow and the voluntary account change.
// On success the server rotates other sessions and clears must_change_password;
// we refetch identity so the app re-renders past the forced screen.
export function ChangePasswordForm({ onDone }: { onDone?: () => void }) {
  const { refetch } = useAuth()
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    if (next.length < 8) return setErr("New password must be at least 8 characters.")
    if (next !== confirm) return setErr("New passwords do not match.")
    setBusy(true)
    try {
      await api("/api/v1/auth/change-password", {
        method: "POST",
        body: JSON.stringify({ current, next }),
      })
      setCurrent("")
      setNext("")
      setConfirm("")
      refetch()
      onDone?.()
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Failed to change password.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="space-y-3">
      <div className="space-y-1.5">
        <Label htmlFor="cur">Current password</Label>
        <Input id="cur" type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="new">New password</Label>
        <Input id="new" type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="cfm">Confirm new password</Label>
        <Input id="cfm" type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" />
      </div>
      {err && <p className="text-sm text-destructive">{err}</p>}
      <Button type="submit" disabled={busy} className="w-full">
        {busy ? "Saving…" : "Change password"}
      </Button>
    </form>
  )
}
