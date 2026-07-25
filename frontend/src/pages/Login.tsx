import { useState, type FormEvent } from "react"
import { api } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { ChangePasswordForm } from "@/components/ChangePasswordForm"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

// Login is the unauthenticated gate. When the signed-in user still has
// must_change_password set, it shows the forced change instead of the app.
export default function Login({ forceChange = false }: { forceChange?: boolean }) {
  const { refetch } = useAuth()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    setBusy(true)
    try {
      await api("/api/v1/auth/login", {
        method: "POST",
        body: JSON.stringify({ username, password }),
      })
      refetch() // cookie is set; re-resolve identity
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Login failed.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-lg">
            {forceChange ? "Set a new password" : "Sign in to EchoMap"}
          </CardTitle>
          <CardDescription>
            {forceChange
              ? "Your account requires a password change before you continue."
              : "Network Topology & Monitoring"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {forceChange ? (
            <ChangePasswordForm />
          ) : (
            <form onSubmit={submit} className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="u">Username</Label>
                <Input id="u" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus autoComplete="username" />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="p">Password</Label>
                <Input id="p" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
              </div>
              {err && <p className="text-sm text-destructive">{err}</p>}
              <Button type="submit" disabled={busy} className="w-full">
                {busy ? "Signing in…" : "Sign in"}
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
