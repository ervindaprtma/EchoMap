import { useState } from "react"
import { Check, ChevronsUpDown, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import type { Device } from "@/lib/types"

// Searchable dependency-parent picker. `excludeIds` = the edited device +
// its descendants, so the UI never offers a cycle (server 400 is the backstop).
export function ParentSelect({
  devices, excludeIds, value, onChange,
}: {
  devices: Device[]
  excludeIds: Set<number>
  value: number | null
  onChange: (id: number | null) => void
}) {
  const [open, setOpen] = useState(false)
  const options = devices.filter((d) => !excludeIds.has(d.id))
  const selected = devices.find((d) => d.id === value)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" role="combobox" className="w-full justify-between font-normal">
          {selected ? `${selected.name} (${selected.ip_address})` : "No parent (independent)"}
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder="Search device…" />
          <CommandList>
            <CommandEmpty>No device found.</CommandEmpty>
            <CommandGroup>
              <CommandItem
                value="__none"
                onSelect={() => { onChange(null); setOpen(false) }}
              >
                <X className="mr-2 h-4 w-4 opacity-50" />
                No parent (independent)
              </CommandItem>
              {options.map((d) => (
                <CommandItem
                  key={d.id}
                  value={`${d.name} ${d.ip_address}`}
                  onSelect={() => { onChange(d.id); setOpen(false) }}
                >
                  <Check className={cn("mr-2 h-4 w-4", value === d.id ? "opacity-100" : "opacity-0")} />
                  {d.name} <span className="ml-1 font-mono text-xs text-muted-foreground">{d.ip_address}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
