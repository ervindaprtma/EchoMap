package snmp

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name                        string
		descr, oid                  string
		vendor, model, icon         string
	}{
		{
			name:  "cisco catalyst switch",
			descr: "Cisco IOS Software, C2960 Software (C2960-LANBASEK9-M), Version 15.0",
			oid:   ".1.3.6.1.4.1.9.1.716",
			vendor: "Cisco", model: "C2960", icon: "router", // "ios" hits router before "switch" keyword absent
		},
		{
			name:  "fortigate firewall",
			descr: "FortiGate-60F v7.2.1,build1254",
			oid:   ".1.3.6.1.4.1.12356.101.1.60",
			vendor: "Fortinet", model: "FortiGate-60F", icon: "firewall",
		},
		{
			name:  "mikrotik router",
			descr: "RouterOS RB750Gr3",
			oid:   ".1.3.6.1.4.1.14988.1",
			vendor: "MikroTik", icon: "router",
		},
		{
			name:  "linux host via net-snmp",
			descr: "Linux gw 5.15.0 #1 SMP x86_64",
			oid:   ".1.3.6.1.4.1.8072.3.2.10",
			vendor: "Linux", icon: "server",
		},
		{
			name:  "unknown vendor falls back to PEN then Unknown",
			descr: "Some Appliance 9000",
			oid:   ".1.3.6.1.4.1.99999.1",
			vendor: "Unknown", icon: "host",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, m, ic := Classify(c.descr, c.oid)
			if v != c.vendor {
				t.Errorf("vendor = %q, want %q", v, c.vendor)
			}
			if c.model != "" && m != c.model {
				t.Errorf("model = %q, want %q", m, c.model)
			}
			if ic != c.icon {
				t.Errorf("icon = %q, want %q", ic, c.icon)
			}
		})
	}
}
