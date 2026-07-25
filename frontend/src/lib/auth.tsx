import { createContext, useContext, type ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api, ApiError } from "@/lib/api"

export type Role = "OPERATOR" | "ADMIN" | "SUPERADMIN"

export interface Me {
  id: number | null
  username: string
  role: Role
  must_change_password: boolean
}

const ROLE_LEVEL: Record<Role, number> = { OPERATOR: 1, ADMIN: 2, SUPERADMIN: 3 }

interface AuthValue {
  me: Me | null
  isLoading: boolean
  refetch: () => void
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()

  const { data, isLoading, refetch } = useQuery<Me | null>({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await api<Me>("/api/v1/auth/me")
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null // not signed in
        throw e
      }
    },
    retry: false,
    staleTime: 30_000,
  })

  const logout = async () => {
    try {
      await api("/api/v1/auth/logout", { method: "POST" })
    } catch {
      /* already gone — clear locally regardless */
    }
    qc.clear() // drop every cached query so no stale data leaks across a sign-in
    await refetch()
  }

  return (
    <AuthContext.Provider value={{ me: data ?? null, isLoading, refetch: () => void refetch(), logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error("useAuth must be used within AuthProvider")
  return ctx
}

// useRole is the presentation gate (PRD §6.7): hide what a role can't use. The API
// remains the enforcement point — this only trims the UI.
export function useRole() {
  const { me } = useAuth()
  const level = me ? ROLE_LEVEL[me.role] : 0
  return {
    role: me?.role ?? null,
    isOperator: level >= 1,
    isAdmin: level >= 2,
    isSuperadmin: level >= 3,
  }
}
