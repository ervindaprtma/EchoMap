// Package snmp fingerprints a device over SNMP: system identity (sysDescr,
// sysObjectID, sysName) plus the interface table, then classifies vendor/model/icon.
package snmp

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Standard MIB-II OIDs (RFC 1213).
const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidIfDescr     = "1.3.6.1.2.1.2.2.1.2" // ifTable.ifDescr (walk)
	oidIfPhysAddr  = "1.3.6.1.2.1.2.2.1.6" // ifTable.ifPhysAddress (walk)
)

const maxInterfaces = 512 // cap the ifTable walk so a huge chassis can't flood us

// Interface is one row of the SNMP ifTable.
type Interface struct {
	Index int    `json:"if_index"`
	Name  string `json:"if_name"`
	MAC   string `json:"mac_address"` // "aa:bb:cc:dd:ee:ff" or ""
}

// Result is a device fingerprint.
type Result struct {
	SysDescr    string      `json:"sys_descr"`
	SysObjectID string      `json:"sys_object_id"`
	SysName     string      `json:"sys_name"`
	Vendor      string      `json:"vendor"`
	Model       string      `json:"model"`
	Icon        string      `json:"icon"`
	Interfaces  []Interface `json:"interfaces"`
}

// Fingerprint queries the device at ip. Only SNMP v2c is supported today (v3 needs
// per-user auth/priv params EchoMap does not store) — an unsupported version is an
// error, not a silent v2c fallback. community must be non-empty.
func Fingerprint(ctx context.Context, ip, version, community string, timeout time.Duration) (*Result, error) {
	if strings.EqualFold(version, "v3") {
		return nil, fmt.Errorf("SNMPv3 is not yet supported (only v2c)")
	}
	if community == "" {
		return nil, fmt.Errorf("no SNMP community configured")
	}

	g := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      161,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   timeout,
		Retries:   1,
		Context:   ctx,
	}
	if err := g.Connect(); err != nil {
		return nil, fmt.Errorf("snmp connect %s: %w", ip, err)
	}
	defer g.Conn.Close()

	sys, err := g.Get([]string{oidSysDescr, oidSysObjectID, oidSysName})
	if err != nil {
		return nil, fmt.Errorf("snmp get %s: %w", ip, err)
	}

	res := &Result{}
	for _, v := range sys.Variables {
		switch v.Name {
		case "." + oidSysDescr:
			res.SysDescr = octetString(v)
		case "." + oidSysObjectID:
			res.SysObjectID = strings.TrimPrefix(oidString(v), ".")
		case "." + oidSysName:
			res.SysName = octetString(v)
		}
	}

	res.Interfaces = walkInterfaces(g)
	res.Vendor, res.Model, res.Icon = Classify(res.SysDescr, res.SysObjectID)
	return res, nil
}

// walkInterfaces builds the ifTable, joining ifDescr and ifPhysAddress by ifIndex.
// A walk failure is non-fatal — identity is still useful without interfaces.
func walkInterfaces(g *gosnmp.GoSNMP) []Interface {
	byIndex := map[int]*Interface{}

	descrs, err := g.BulkWalkAll(oidIfDescr)
	if err != nil {
		return nil
	}
	for _, pdu := range descrs {
		idx := lastOIDElem(pdu.Name)
		if idx < 0 || len(byIndex) >= maxInterfaces {
			continue
		}
		byIndex[idx] = &Interface{Index: idx, Name: octetString(pdu)}
	}

	macs, err := g.BulkWalkAll(oidIfPhysAddr)
	if err == nil {
		for _, pdu := range macs {
			if iface, ok := byIndex[lastOIDElem(pdu.Name)]; ok {
				iface.MAC = formatMAC(pdu)
			}
		}
	}

	out := make([]Interface, 0, len(byIndex))
	for _, iface := range byIndex {
		out = append(out, *iface)
	}
	return out
}

func octetString(v gosnmp.SnmpPDU) string {
	if b, ok := v.Value.([]byte); ok {
		return strings.TrimSpace(string(b))
	}
	if s, ok := v.Value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func oidString(v gosnmp.SnmpPDU) string {
	if s, ok := v.Value.(string); ok {
		return s
	}
	return ""
}

func formatMAC(v gosnmp.SnmpPDU) string {
	b, ok := v.Value.([]byte)
	if !ok || len(b) == 0 {
		return ""
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = hex.EncodeToString([]byte{c})
	}
	return strings.Join(parts, ":")
}

func lastOIDElem(name string) int {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return -1
	}
	n, err := strconv.Atoi(name[i+1:])
	if err != nil {
		return -1
	}
	return n
}
