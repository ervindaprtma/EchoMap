// Browser sound + desktop pop-up alerts (Doc 4 §6, The Dude-style).
// Per-browser preferences in localStorage — nothing server-side, so each operator
// decides for their own workstation. Fired from the app shell's WS handler, so
// they work on any page.
//
// Which SOUND plays is a server-side per-event-class assignment (Pillar 14): an
// uploaded .wav or, when unassigned, the built-in synthesized chime below.

import { api } from "@/lib/api"

export interface AlertPrefs {
  sound: boolean
  popup: boolean
}

const PREFS_KEY = "echomap.alertPrefs"
const DEFAULTS: AlertPrefs = { sound: false, popup: false }

export function loadPrefs(): AlertPrefs {
  try {
    return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(PREFS_KEY) ?? "{}") }
  } catch {
    return DEFAULTS // corrupt/blocked storage: stay quiet rather than throw in the WS handler
  }
}

export function savePrefs(p: AlertPrefs) {
  localStorage.setItem(PREFS_KEY, JSON.stringify(p))
}

export type AlertKind = "down" | "up"

export interface AlertableEvent {
  to_status: string
  from_status?: string
  down_reason?: string | null
}

// alertKind decides whether a status event should interrupt a human, mirroring
// the backend's alert suppression (Doc 3 §5): cascade children stay silent —
// only the parent that actually failed sounds — and so do UNKNOWN resets and
// first-sighting UNKNOWN->UP. Recovery is only announced for a device that was
// really DOWN, so a cascade victim released to UNKNOWN then probed UP is quiet.
export function alertKind(ev: AlertableEvent): AlertKind | null {
  if (ev.to_status === "DOWN") return ev.down_reason === "PARENT" ? null : "down"
  if (ev.to_status === "UP" && ev.from_status === "DOWN") return "up"
  return null
}

let ac: AudioContext | undefined

// playChime synthesizes the tone instead of shipping an audio file: two notes,
// falling for DOWN, rising for recovery. Callers on a user gesture also unlock
// playback — browsers suspend a fresh AudioContext until one happens.
export function playChime(kind: AlertKind) {
  try {
    ac ??= new AudioContext()
    if (ac.state === "suspended") void ac.resume()
    const notes = kind === "down" ? [740, 370] : [523, 784]
    notes.forEach((freq, i) => beep(ac!, freq, i * 0.18, 0.16))
  } catch {
    // no WebAudio (or blocked): a missing beep must never break the event stream
  }
}

// ---- custom uploaded sounds (Pillar 14) --------------------------------------
// Assignment of an uploaded sound id to each event class, cached from
// GET /settings/sounds and refreshed on login + after a Settings save.
export type SoundEvent = "device_down" | "device_up" | "monitor_down" | "monitor_up"

let soundIds: Record<SoundEvent, number | null> = {
  device_down: null, device_up: null, monitor_down: null, monitor_up: null,
}
const soundBlobs = new Map<number, string>() // sound id -> object URL (fetched once)

function clearBlobs() {
  soundBlobs.forEach((url) => URL.revokeObjectURL(url))
  soundBlobs.clear()
}

export async function refreshSoundAssignments(): Promise<void> {
  try {
    const a = await api<{
      device_down_id: number | null
      device_up_id: number | null
      monitor_down_id: number | null
      monitor_up_id: number | null
    }>("/api/v1/settings/sounds")
    soundIds = {
      device_down: a.device_down_id, device_up: a.device_up_id,
      monitor_down: a.monitor_down_id, monitor_up: a.monitor_up_id,
    }
    clearBlobs() // a reassigned class may now point at different bytes
  } catch {
    // keep whatever we have — the chime fallback always works
  }
}

// playForEvent plays the assigned .wav for an event class, or the chime if none
// is assigned (or the fetch/playback fails — the fallback must never be missed).
function playForEvent(evt: SoundEvent, kind: AlertKind) {
  const id = soundIds[evt]
  if (id == null) {
    playChime(kind)
    return
  }
  void playSoundFile(id).catch(() => playChime(kind))
}

// playSoundFile fetches with the session cookie (an <audio src> can't), caches
// the blob URL, and plays it. Exported so the Settings preview button reuses it.
export async function playSoundFile(id: number): Promise<void> {
  let url = soundBlobs.get(id)
  if (!url) {
    const res = await fetch(`/api/v1/sounds/${id}/audio`, { credentials: "include" })
    if (!res.ok) throw new Error(`sound ${id} fetch ${res.status}`)
    url = URL.createObjectURL(await res.blob())
    soundBlobs.set(id, url)
  }
  await new Audio(url).play()
}

function beep(ctx: AudioContext, freq: number, delay: number, dur: number) {
  const osc = ctx.createOscillator()
  const gain = ctx.createGain()
  osc.type = "sine"
  osc.frequency.value = freq
  osc.connect(gain).connect(ctx.destination)
  // Ramped envelope: a raw start/stop on a sine clicks audibly.
  const t = ctx.currentTime + delay
  gain.gain.setValueAtTime(0, t)
  gain.gain.linearRampToValueAtTime(0.18, t + 0.02)
  gain.gain.linearRampToValueAtTime(0, t + dur)
  osc.start(t)
  osc.stop(t + dur + 0.02)
}

interface StatusFrame extends AlertableEvent {
  device_id: number
  name?: string
  ip_address?: string
}

// fireAlert is the single entry point from the WS handler: it reads the live
// prefs each time (cheap, and picks up a toggle flipped in another tab).
export function fireAlert(ev: StatusFrame) {
  const kind = alertKind(ev)
  if (!kind) return
  const prefs = loadPrefs()
  if (prefs.sound) playForEvent(kind === "down" ? "device_down" : "device_up", kind)
  if (prefs.popup) showPopup(kind, ev)
}

function showPopup(kind: AlertKind, ev: StatusFrame) {
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return
  const who = ev.name ?? ev.ip_address ?? `device ${ev.device_id}`
  try {
    new Notification(kind === "down" ? `DOWN: ${who}` : `Recovered: ${who}`, {
      body: kind === "down" ? `${ev.ip_address ?? ""} stopped responding to ping`.trim() : `${ev.ip_address ?? ""} is back UP`.trim(),
      // One live pop-up per device: a flapping host replaces its own notice
      // instead of stacking a wall of them.
      tag: `echomap-device-${ev.device_id}`,
    })
  } catch {
    // Notification constructor throws on some mobile browsers — ignore.
  }
}

// MonitorFrame is the `monitor.status` WS frame (Doc 5 §8.7).
export interface MonitorFrame {
  monitor_id: number
  device_id: number
  label?: string
  kind?: string
  to_status: string
  from_status?: string
}

// fireMonitorAlert mirrors fireAlert for custom monitors. Monitors have no
// PARENT cascade, so the rule is simpler: DOWN sounds, and UP is announced only
// as a recovery from DOWN (a first-sighting UNKNOWN→UP stays quiet).
export function fireMonitorAlert(ev: MonitorFrame) {
  let kind: AlertKind | null = null
  if (ev.to_status === "DOWN") kind = "down"
  else if (ev.to_status === "UP" && ev.from_status === "DOWN") kind = "up"
  if (!kind) return
  const prefs = loadPrefs()
  if (prefs.sound) playForEvent(kind === "down" ? "monitor_down" : "monitor_up", kind)
  if (prefs.popup) showMonitorPopup(kind, ev)
}

function showMonitorPopup(kind: AlertKind, ev: MonitorFrame) {
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return
  const what = ev.label ?? `monitor ${ev.monitor_id}`
  try {
    new Notification(kind === "down" ? `Monitor DOWN: ${what}` : `Monitor recovered: ${what}`, {
      body: `${ev.kind ?? ""} check ${kind === "down" ? "failed" : "is passing again"}`.trim(),
      tag: `echomap-monitor-${ev.monitor_id}`, // one live pop-up per monitor
    })
  } catch {
    /* Notification constructor throws on some mobile browsers — ignore. */
  }
}

// requestPopupPermission is called from the toggle (a user gesture, which the
// permission prompt requires). Returns whether pop-ups can actually be shown.
export async function requestPopupPermission(): Promise<boolean> {
  if (typeof Notification === "undefined") return false
  if (Notification.permission === "granted") return true
  if (Notification.permission === "denied") return false
  return (await Notification.requestPermission()) === "granted"
}
