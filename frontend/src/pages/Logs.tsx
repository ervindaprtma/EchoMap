import { useState } from "react"
import { useQuery, keepPreviousData } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

interface EventLog {
  id: number
  ts: string
  level: string
  category: string
  message: string
  device_id: number | null
  user_id: number | null
  meta: Record<string, unknown> | null
}

interface LogPage {
  items: EventLog[]
  total: number
  page: number
  page_size: number
}

const LEVELS = ["INFO", "NOTICE", "ALERT", "ERROR", "AUDIT"]
// Level → chip colors (Doc 4 §6.8): INFO gray · NOTICE blue · ALERT amber · ERROR red · AUDIT violet.
const levelClass: Record<string, string> = {
  INFO: "bg-muted text-muted-foreground",
  NOTICE: "bg-blue-500/15 text-blue-600 dark:text-blue-400",
  ALERT: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  ERROR: "bg-red-500/15 text-red-600 dark:text-red-400",
  AUDIT: "bg-violet-500/15 text-violet-600 dark:text-violet-400",
}

const PAGE_SIZE = 50
const ALL = "__all__" // Radix Select can't hold an empty-string value

export default function Logs() {
  const [level, setLevel] = useState("")
  const [category, setCategory] = useState("")
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)

  const params = new URLSearchParams()
  if (level) params.set("level", level)
  if (category) params.set("category", category)
  if (search) params.set("search", search)
  params.set("page", String(page))
  params.set("page_size", String(PAGE_SIZE))

  const { data } = useQuery<LogPage>({
    queryKey: ["logs", level, category, search, page],
    queryFn: () => api(`/api/v1/logs?${params.toString()}`),
    placeholderData: keepPreviousData,
    refetchInterval: 15_000, // poll: "live enough" without a per-row WS stream (see as-built note)
  })

  const total = data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const reset = <T,>(setter: (v: T) => void) => (v: T) => { setter(v); setPage(1) }

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">Event Logs</h2>

      <div className="flex flex-wrap items-center gap-2">
        <Select value={level || ALL} onValueChange={reset((v: string) => setLevel(v === ALL ? "" : v))}>
          <SelectTrigger className="h-9 w-40"><SelectValue placeholder="All levels" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All levels</SelectItem>
            {LEVELS.map((l) => <SelectItem key={l} value={l}>{l}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={category || ALL} onValueChange={reset((v: string) => setCategory(v === ALL ? "" : v))}>
          <SelectTrigger className="h-9 w-40"><SelectValue placeholder="All categories" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All categories</SelectItem>
            {["device", "monitor", "alert", "auth", "config", "system"].map((c) => (
              <SelectItem key={c} value={c}>{c}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          className="h-9 w-64"
          placeholder="Search message…"
          value={search}
          onChange={(e) => { setSearch(e.target.value); setPage(1) }}
        />
        <span className="ml-auto text-sm text-muted-foreground">{total} entries</span>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-44">Time</TableHead>
            <TableHead className="w-24">Level</TableHead>
            <TableHead className="w-28">Category</TableHead>
            <TableHead>Message</TableHead>
            <TableHead className="w-16 text-right">Meta</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {data?.items?.length ? (
            data.items.map((l) => (
              <TableRow key={l.id}>
                <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                  {new Date(l.ts).toLocaleString()}
                </TableCell>
                <TableCell>
                  <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${levelClass[l.level] ?? ""}`}>
                    {l.level}
                  </span>
                </TableCell>
                <TableCell className="text-sm">{l.category}</TableCell>
                <TableCell className="text-sm">{l.message}</TableCell>
                <TableCell className="text-right">
                  {l.meta && Object.keys(l.meta).length > 0 && (
                    <Popover>
                      <PopoverTrigger asChild>
                        <Button size="sm" variant="ghost">view</Button>
                      </PopoverTrigger>
                      <PopoverContent align="end" className="w-80">
                        <pre className="overflow-x-auto text-xs">{JSON.stringify(l.meta, null, 2)}</pre>
                      </PopoverContent>
                    </Popover>
                  )}
                </TableCell>
              </TableRow>
            ))
          ) : (
            <TableRow>
              <TableCell colSpan={5} className="py-8 text-center text-sm text-muted-foreground">
                No log entries match.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      <div className="flex items-center justify-end gap-2 text-sm">
        <Button size="sm" variant="ghost" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Prev</Button>
        <span className="text-muted-foreground">Page {page} / {pages}</span>
        <Button size="sm" variant="ghost" disabled={page >= pages} onClick={() => setPage((p) => p + 1)}>Next</Button>
      </div>
    </div>
  )
}
