// Thin fetch wrapper. Phase 8: the browser authenticates by the HttpOnly
// `echomap_session` cookie (credentials: "include"), minted by POST /auth/login —
// no bearer token in JS. The static APP_API_TOKEN remains for headless automation
// only. A 401 means "no/expired session"; callers surface it as the login screen.

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
    this.name = "ApiError"
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: "include", // send/receive the session cookie
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  })
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try {
      const body = await res.json()
      if (body?.error) msg = body.error
    } catch {
      /* non-JSON error body */
    }
    throw new ApiError(res.status, msg)
  }
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T)
}
