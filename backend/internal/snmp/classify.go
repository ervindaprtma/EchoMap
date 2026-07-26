package snmp

import (
	"regexp"
	"strings"
)

// Classify derives (vendor, model, icon) from a device's SNMP sysDescr and
// sysObjectID. This is deliberately a heuristic — SNMP has no universal "model"
// OID, so we keyword-match the free-text sysDescr and fall back gracefully.
//
// ponytail: a keyword table + a couple of per-vendor model regexes. Upgrade path
// if accuracy matters: map sysObjectID's enterprise arc (1.3.6.1.4.1.<N>) through
// an IANA PEN table for the vendor, and ship a real model lookup per platform.
func Classify(sysDescr, sysObjectID string) (vendor, model, icon string) {
	d := strings.ToLower(sysDescr)

	for _, v := range vendorTable {
		if strings.Contains(d, v.needle) {
			vendor = v.name
			break
		}
	}
	if vendor == "" {
		// Enterprise PEN fallback for the handful we recognize by OID.
		vendor = vendorFromOID(sysObjectID)
	}

	model = extractModel(vendor, sysDescr)
	icon = iconFor(d, sysObjectID)
	return vendor, model, icon
}

type vendorMatch struct{ needle, name string }

// Ordered: more specific needles first (e.g. "fortigate" before a bare "linux").
var vendorTable = []vendorMatch{
	{"cisco", "Cisco"},
	{"juniper", "Juniper"},
	{"junos", "Juniper"},
	{"mikrotik", "MikroTik"},
	{"routeros", "MikroTik"},
	{"arista", "Arista"},
	{"fortigate", "Fortinet"},
	{"fortinet", "Fortinet"},
	{"palo alto", "Palo Alto"},
	{"pan-os", "Palo Alto"},
	{"ubiquiti", "Ubiquiti"},
	{"edgeos", "Ubiquiti"},
	{"unifi", "Ubiquiti"},
	{"huawei", "Huawei"},
	{"hewlett", "HP"},
	{"procurve", "HP"},
	{"aruba", "Aruba"},
	{"dell", "Dell"},
	{"netgear", "Netgear"},
	{"vyos", "VyOS"},
	{"pfsense", "Netgate"},
	{"linux", "Linux"},
	{"windows", "Microsoft"},
}

// A few well-known enterprise PENs (1.3.6.1.4.1.<PEN>) as an OID fallback.
var penTable = map[string]string{
	"9":     "Cisco",
	"2636":  "Juniper",
	"14988": "MikroTik",
	"30065": "Arista",
	"12356": "Fortinet",
	"25461": "Palo Alto",
	"41112": "Ubiquiti",
	"2011":  "Huawei",
	"11":    "HP",
	"14823": "Aruba",
	"8072":  "Net-SNMP",
}

var penRE = regexp.MustCompile(`1\.3\.6\.1\.4\.1\.(\d+)`)

func vendorFromOID(oid string) string {
	if m := penRE.FindStringSubmatch(oid); m != nil {
		if v, ok := penTable[m[1]]; ok {
			return v
		}
	}
	return "Unknown"
}

// Model heuristics per vendor. Each grabs the most model-looking token.
var (
	ciscoModelRE = regexp.MustCompile(`(?i)\b(WS-C\w+|C\d{4}\w*|ASR\d+\w*|ISR\d+\w*|Nexus\s?\w+|Catalyst\s?\w+|ASA\d+\w*)\b`)
	fortiModelRE = regexp.MustCompile(`(?i)\b(FortiGate-?\w+|FGT\w+|FortiSwitch-?\w+)\b`)
	genericRE    = regexp.MustCompile(`(?i)\b([A-Z]{1,4}[-]?\d{3,}[A-Z0-9-]*)\b`)
)

func extractModel(vendor, sysDescr string) string {
	switch vendor {
	case "Cisco":
		if m := ciscoModelRE.FindString(sysDescr); m != "" {
			return strings.TrimSpace(m)
		}
	case "Fortinet":
		if m := fortiModelRE.FindString(sysDescr); m != "" {
			return strings.TrimSpace(m)
		}
	}
	if m := genericRE.FindString(sysDescr); m != "" {
		return strings.TrimSpace(m)
	}
	return ""
}

// iconFor picks a builtin icon key from sysDescr keywords. Builtin keys the UI
// renders: router / switch / firewall / server / host (default).
func iconFor(lowerDescr, _ string) string {
	switch {
	case strings.Contains(lowerDescr, "firewall"),
		strings.Contains(lowerDescr, "fortigate"),
		strings.Contains(lowerDescr, "asa"),
		strings.Contains(lowerDescr, "pan-os"),
		strings.Contains(lowerDescr, "pfsense"):
		return "firewall"
	case strings.Contains(lowerDescr, "switch"),
		strings.Contains(lowerDescr, "catalyst"),
		strings.Contains(lowerDescr, "nexus"),
		strings.Contains(lowerDescr, "procurve"):
		return "switch"
	case strings.Contains(lowerDescr, "router"),
		strings.Contains(lowerDescr, "routeros"),
		strings.Contains(lowerDescr, "ios"),
		strings.Contains(lowerDescr, "junos"):
		return "router"
	case strings.Contains(lowerDescr, "linux"),
		strings.Contains(lowerDescr, "windows"),
		strings.Contains(lowerDescr, "server"):
		return "server"
	default:
		return "host"
	}
}
