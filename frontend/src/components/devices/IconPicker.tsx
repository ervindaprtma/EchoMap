import { useState } from "react"
import { ChevronsUpDown } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import type { IconRow } from "@/lib/types"

// Custom-icon override picker (devices.icon_id). "Auto" clears the override so
// the builtin key (from SNMP fingerprinting) applies again.
export function IconPicker({
  icons, value, onChange,
}: {
  icons: IconRow[]
  value: number | null
  onChange: (id: number | null) => void
}) {
  const [open, setOpen] = useState(false)
  const selected = icons.find((i) => i.id === value)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" className="w-full justify-between font-normal">
          <span className="flex items-center gap-2">
            {selected && <img src={selected.data_url} alt="" className="h-4 w-4" />}
            {selected ? selected.name : "Auto (built-in icon)"}
          </span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-2">
        <button
          type="button"
          onClick={() => { onChange(null); setOpen(false) }}
          className={cn(
            "mb-2 w-full rounded border px-2 py-1 text-left text-sm hover:bg-muted",
            value === null && "border-primary",
          )}
        >
          Auto (built-in icon)
        </button>
        {icons.length === 0 ? (
          <p className="px-1 py-2 text-xs text-muted-foreground">
            No custom icons yet — upload SVG/PNG in the Icon Library.
          </p>
        ) : (
          <div className="grid max-h-56 grid-cols-4 gap-1 overflow-y-auto">
            {icons.map((i) => (
              <button
                key={i.id}
                type="button"
                title={i.name}
                onClick={() => { onChange(i.id); setOpen(false) }}
                className={cn(
                  "flex aspect-square items-center justify-center rounded border p-1.5 hover:bg-muted",
                  value === i.id && "border-primary bg-muted",
                )}
              >
                <img src={i.data_url} alt={i.name} className="max-h-full max-w-full" />
              </button>
            ))}
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}
