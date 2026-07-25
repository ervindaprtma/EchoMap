/// <reference types="vite/client" />

interface ImportMetaEnv {
  // Dev-only bearer token (must match APP_API_TOKEN). Replaced by session login in Slice 7.
  readonly VITE_API_TOKEN?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
