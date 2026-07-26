import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { NavLink, Route, Routes } from "react-router-dom"
import { refreshSoundAssignments } from "@/lib/alerts"
import { AlertSettings } from "@/components/AlertSettings"
import { AccountMenu } from "@/components/AccountMenu"
import { AuthProvider, useAuth, useRole } from "@/lib/auth"
import Login from "@/pages/Login"
import Devices from "@/pages/Devices"
import { History } from "@/pages/History"
import Topology from "@/pages/Topology"
import Users from "@/pages/Users"
import Logs from "@/pages/Logs"
import Settings from "@/pages/Settings"
import { useRealtime } from "@/lib/realtime"

const navClass = ({ isActive }: { isActive: boolean }) =>
  `rounded px-2.5 py-1 text-sm ${isActive ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground"}`

export default function App() {
  return (
    <AuthProvider>
      <Gate />
    </AuthProvider>
  )
}

// Gate resolves identity before rendering the app: loading → spinner,
// unauthenticated → Login, forced-change → Login(forceChange), else the shell.
function Gate() {
  const { me, isLoading } = useAuth()
  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    )
  }
  if (!me) return <Login />
  if (me.must_change_password) return <Login forceChange />
  return <Shell />
}

function Shell() {
  const live = useRealtime()
  const { isAdmin, isSuperadmin } = useRole()
  // Load which uploaded sound plays for each alert class (Pillar 14); falls back
  // to the built-in chime until (and if) it resolves.
  useEffect(() => { void refreshSoundAssignments() }, [])
  const health = useQuery({
    queryKey: ["health"],
    queryFn: async () => {
      const r = await fetch("/healthz")
      if (!r.ok) throw new Error("unhealthy")
      return (await r.json()) as { status: string }
    },
    refetchInterval: 10_000,
  })

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b">
        <div className="container flex items-center justify-between py-3">
          <div className="flex items-center gap-6">
            <div className="flex items-baseline gap-2">
              <h1 className="text-xl font-bold tracking-tight">EchoMap</h1>
              <span className="hidden text-xs text-muted-foreground sm:inline">
                Network Topology &amp; Monitoring
              </span>
            </div>
            <nav className="flex items-center gap-1">
              <NavLink to="/" end className={navClass}>Devices</NavLink>
              <NavLink to="/topology" className={navClass}>Topology</NavLink>
              {isAdmin && <NavLink to="/logs" className={navClass}>Logs</NavLink>}
              {isAdmin && <NavLink to="/settings" className={navClass}>Settings</NavLink>}
              {isSuperadmin && <NavLink to="/users" className={navClass}>Users</NavLink>}
            </nav>
          </div>
          <div className="flex items-center gap-4 text-xs">
            <AlertSettings />
            <span className="flex items-center gap-1.5">
              <span
                className={`h-2 w-2 rounded-full ${
                  health.isError ? "bg-red-500" : health.isSuccess ? "bg-green-500" : "bg-muted-foreground"
                }`}
              />
              API {health.isError ? "offline" : (health.data?.status ?? "…")}
            </span>
            <span className="flex items-center gap-1.5">
              <span className={`h-2 w-2 rounded-full ${live ? "bg-green-500" : "bg-muted-foreground"}`} />
              {live ? "live" : "offline"}
            </span>
            <AccountMenu />
          </div>
        </div>
      </header>
      <main className="container py-6">
        <Routes>
          <Route path="/" element={<Devices />} />
          <Route path="/devices/:id/history" element={<History />} />
          <Route path="/topology" element={<Topology />} />
          <Route path="/topology/:mapId" element={<Topology />} />
          {isAdmin && <Route path="/logs" element={<Logs />} />}
          {isAdmin && <Route path="/settings" element={<Settings />} />}
          {isSuperadmin && <Route path="/users" element={<Users />} />}
        </Routes>
      </main>
    </div>
  )
}
