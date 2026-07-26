// Package sysmon collects EchoMap's own resource stats — host, containers, and
// services — for Pillar 16. It reads the host through /proc (not cgroup-
// namespaced, so a container sees host totals) and needs no privileges for the
// host + self surfaces. Docker/nginx scrapers land in later slices.
package sysmon

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// HostStats is one host sample. Disk is populated only when /hostfs is mounted.
type HostStats struct {
	CPUPct              float64
	MemUsed, MemTotal   uint64
	SwapUsed, SwapTotal uint64
	Load1, Load5, Load15 float64
	UptimeS             float64
	DiskUsed, DiskTotal uint64
	HasDisk             bool
}

// HostReader reads /proc. CPU% is a delta of jiffies between Read calls, so the
// first Read reports 0 — the caller primes it once before the first tick.
type HostReader struct {
	prevIdle, prevTotal uint64
	hostfs              string // "/hostfs" when the optional bind mount exists, else ""
}

func NewHostReader() *HostReader {
	hr := &HostReader{}
	if fi, err := os.Stat("/hostfs"); err == nil && fi.IsDir() {
		hr.hostfs = "/hostfs"
	}
	return hr
}

func (hr *HostReader) HasHostfs() bool { return hr.hostfs != "" }

func (hr *HostReader) Read() HostStats {
	var s HostStats

	idle, total := readCPU()
	if hr.prevTotal != 0 && total > hr.prevTotal {
		dt := float64(total - hr.prevTotal)
		di := float64(idle - hr.prevIdle)
		s.CPUPct = (1 - di/dt) * 100
	}
	hr.prevIdle, hr.prevTotal = idle, total

	mem := readKV("/proc/meminfo") // values in kB
	s.MemTotal = mem["MemTotal"] * 1024
	if avail, ok := mem["MemAvailable"]; ok {
		s.MemUsed = (mem["MemTotal"] - avail) * 1024
	}
	s.SwapTotal = mem["SwapTotal"] * 1024
	s.SwapUsed = (mem["SwapTotal"] - mem["SwapFree"]) * 1024

	s.Load1, s.Load5, s.Load15 = readLoadavg()
	s.UptimeS = readUptime()

	if hr.hostfs != "" {
		var st syscall.Statfs_t
		if syscall.Statfs(hr.hostfs, &st) == nil {
			bs := uint64(st.Bsize)
			s.DiskTotal = st.Blocks * bs
			s.DiskUsed = (st.Blocks - st.Bfree) * bs
			s.HasDisk = true
		}
	}
	return s
}

// readCPU returns cumulative idle and total jiffies from /proc/stat's first line.
func readCPU() (idle, total uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if sc.Scan() {
		fields := strings.Fields(sc.Text()) // cpu user nice system idle iowait irq softirq steal ...
		if len(fields) >= 8 && fields[0] == "cpu" {
			for i, f := range fields[1:] {
				v, _ := strconv.ParseUint(f, 10, 64)
				total += v
				if i == 3 || i == 4 { // idle + iowait
					idle += v
				}
			}
		}
	}
	return
}

// readKV parses "Key: value kB" lines (e.g. /proc/meminfo) into value-by-key.
func readKV(path string) map[string]uint64 {
	out := map[string]uint64{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		out[strings.TrimSpace(k)], _ = strconv.ParseUint(fields[0], 10, 64)
	}
	return out
}

func readLoadavg() (l1, l5, l15 float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	fields := strings.Fields(string(b))
	if len(fields) >= 3 {
		l1, _ = strconv.ParseFloat(fields[0], 64)
		l5, _ = strconv.ParseFloat(fields[1], 64)
		l15, _ = strconv.ParseFloat(fields[2], 64)
	}
	return
}

func readUptime() float64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}
