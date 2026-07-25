import { useState } from "react"
import { Bell, BellOff } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Switch } from "@/components/ui/switch"
import { loadPrefs, playChime, requestPopupPermission, savePrefs, type AlertPrefs } from "@/lib/alerts"

// AlertSettings is the per-browser sound/pop-up toggle pair (Doc 4 §6). It lives
// in the topbar rather than a Settings page so the shell that fires the alerts
// also owns the switch; the Settings page can reuse loadPrefs/savePrefs later.
export function AlertSettings() {
  const [prefs, setPrefs] = useState<AlertPrefs>(loadPrefs)
  const [denied, setDenied] = useState(false)

  const update = (next: AlertPrefs) => {
    setPrefs(next)
    savePrefs(next)
  }

  const toggleSound = (on: boolean) => {
    update({ ...prefs, sound: on })
    // This click is the user gesture browsers require before audio can play, so
    // unlock the AudioContext here and confirm audibly that it works.
    if (on) playChime("up")
  }

  const togglePopup = async (on: boolean) => {
    if (!on) {
      update({ ...prefs, popup: false })
      return
    }
    const granted = await requestPopupPermission()
    setDenied(!granted)
    update({ ...prefs, popup: granted })
  }

  const anyOn = prefs.sound || prefs.popup

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm" className="h-7 gap-1.5 px-2" title="Alert notifications">
          {anyOn ? <Bell className="h-3.5 w-3.5" /> : <BellOff className="h-3.5 w-3.5 text-muted-foreground" />}
          <span className="text-xs">Alerts</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 space-y-4">
        <div className="space-y-1">
          <p className="text-sm font-medium">Browser alerts</p>
          <p className="text-xs text-muted-foreground">
            Stored in this browser only. Devices down via a dependency parent stay silent.
          </p>
        </div>
        <div className="flex items-center justify-between gap-3">
          <Label htmlFor="alert-sound" className="text-sm font-normal">Play sound on DOWN</Label>
          <Switch id="alert-sound" checked={prefs.sound} onCheckedChange={toggleSound} />
        </div>
        <div className="flex items-center justify-between gap-3">
          <Label htmlFor="alert-popup" className="text-sm font-normal">Desktop pop-ups</Label>
          <Switch id="alert-popup" checked={prefs.popup} onCheckedChange={togglePopup} />
        </div>
        {denied && (
          <p className="text-xs text-destructive">
            Notifications are blocked for this site — allow them in your browser settings.
          </p>
        )}
      </PopoverContent>
    </Popover>
  )
}
