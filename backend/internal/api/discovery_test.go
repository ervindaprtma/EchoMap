package api

import "testing"

// expandCIDR is the sweep's only non-trivial pure logic: correct host counts and
// network/broadcast handling across prefix edge cases.
func TestExpandCIDR(t *testing.T) {
	cases := []struct {
		cidr        string
		want        int
		first, last string
	}{
		{"10.0.0.0/24", 254, "10.0.0.1", "10.0.0.254"}, // network+broadcast excluded
		{"192.168.1.0/30", 2, "192.168.1.1", "192.168.1.2"},
		{"10.2.2.0/31", 2, "10.2.2.0", "10.2.2.1"}, // RFC 3021 point-to-point: both usable
		{"10.1.1.5/32", 1, "10.1.1.5", "10.1.1.5"}, // single host
		{"10.9.9.7/24", 254, "10.9.9.1", "10.9.9.254"}, // unmasked input is masked first
	}
	for _, c := range cases {
		ips, err := expandCIDR(c.cidr)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.cidr, err)
		}
		if len(ips) != c.want {
			t.Fatalf("%s: got %d hosts, want %d", c.cidr, len(ips), c.want)
		}
		if ips[0] != c.first || ips[len(ips)-1] != c.last {
			t.Fatalf("%s: range %s..%s, want %s..%s", c.cidr, ips[0], ips[len(ips)-1], c.first, c.last)
		}
	}

	if _, err := expandCIDR("10.0.0.0/8"); err == nil {
		t.Fatal("expected /8 to be rejected as too large")
	}
	if _, err := expandCIDR("2001:db8::/64"); err == nil {
		t.Fatal("expected IPv6 to be rejected")
	}
	if _, err := expandCIDR("not-a-cidr"); err == nil {
		t.Fatal("expected an invalid-CIDR error")
	}
}
