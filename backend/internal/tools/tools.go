// Package tools implements the on-demand network tools (Doc 5 §4): traceroute
// and DNS lookup. Single-shot ping reuses internal/ping. These run in the api
// role under its NET_RAW capability, synchronously and short-capped.
package tools

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type Hop struct {
	TTL      int      `json:"ttl"`
	IP       *string  `json:"ip"`       // nil = no answer within the per-hop timeout ("*")
	Hostname *string  `json:"hostname"` // reverse DNS, best-effort
	RTTMs    *float64 `json:"rtt_ms"`
}

// Traceroute sends ICMP echoes with increasing TTL: routers on the path answer
// TimeExceeded, the destination answers EchoReply. One probe per hop —
// operators re-run for confirmation; three probes would triple the wait.
func Traceroute(ctx context.Context, target string, maxHops int, perHop time.Duration) ([]Hop, string, error) {
	dst, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		return nil, "", err
	}
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil, "", err
	}
	defer conn.Close()
	pc := conn.IPv4PacketConn()

	id := os.Getpid() & 0xffff
	hops := []Hop{}

	for ttl := 1; ttl <= maxHops; ttl++ {
		if ctx.Err() != nil {
			return hops, dst.IP.String(), nil // client gave up; return what we have
		}
		_ = pc.SetTTL(ttl)
		seq := ttl
		wb, err := (&icmp.Message{
			Type: ipv4.ICMPTypeEcho, Code: 0,
			Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("echomap-tr")},
		}).Marshal(nil)
		if err != nil {
			return hops, dst.IP.String(), err
		}

		start := time.Now()
		if _, err := conn.WriteTo(wb, dst); err != nil {
			return hops, dst.IP.String(), err // e.g. missing CAP_NET_RAW
		}
		_ = conn.SetReadDeadline(start.Add(perHop))

		hop := Hop{TTL: ttl}
		reached := false
		rb := make([]byte, 1500)
	read:
		for {
			n, peer, err := conn.ReadFrom(rb)
			if err != nil {
				break // per-hop timeout -> "*"
			}
			rm, err := icmp.ParseMessage(ipv4.ICMPTypeEcho.Protocol(), rb[:n])
			if err != nil {
				continue
			}
			switch rm.Type {
			case ipv4.ICMPTypeTimeExceeded:
				body, ok := rm.Body.(*icmp.TimeExceeded)
				if !ok || !innerEchoMatches(body.Data, id, seq) {
					continue // someone else's probe (raw sockets see everything)
				}
				ip, rtt := peer.String(), msSince(start)
				hop.IP, hop.RTTMs = &ip, &rtt
				break read
			case ipv4.ICMPTypeEchoReply:
				echo, ok := rm.Body.(*icmp.Echo)
				if !ok || echo.ID != id || echo.Seq != seq || peer.String() != dst.IP.String() {
					continue
				}
				ip, rtt := peer.String(), msSince(start)
				hop.IP, hop.RTTMs = &ip, &rtt
				reached = true
				break read
			default:
				continue
			}
		}

		if hop.IP != nil {
			hop.Hostname = reverseDNS(ctx, *hop.IP)
		}
		hops = append(hops, hop)
		if reached {
			break
		}
	}
	return hops, dst.IP.String(), nil
}

// innerEchoMatches digs into a TimeExceeded payload (original IPv4 header +
// first 8 bytes of our echo) and checks it was OUR probe.
func innerEchoMatches(data []byte, id, seq int) bool {
	if len(data) < 1 {
		return false
	}
	ihl := int(data[0]&0x0f) * 4
	if len(data) < ihl+8 {
		return false
	}
	inner := data[ihl:]
	return inner[0] == 8 && // ICMP echo request
		int(binary.BigEndian.Uint16(inner[4:6])) == id &&
		int(binary.BigEndian.Uint16(inner[6:8])) == seq
}

func msSince(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}

func reverseDNS(ctx context.Context, ip string) *string {
	rctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(rctx, ip)
	if err != nil || len(names) == 0 {
		return nil
	}
	return &names[0]
}

type DNSResult struct {
	Name  string   `json:"name"`
	IPs   []string `json:"ips"`
	CNAME string   `json:"cname,omitempty"`
	PTR   []string `json:"ptr,omitempty"`
}

// DNSLookup is the nslookup equivalent: A/AAAA + CNAME for a name, PTR for an IP.
func DNSLookup(ctx context.Context, name string) (DNSResult, error) {
	res := DNSResult{Name: name}
	r := net.DefaultResolver

	if net.ParseIP(name) != nil {
		res.IPs = []string{name}
		if ptr, err := r.LookupAddr(ctx, name); err == nil {
			res.PTR = ptr
		}
		return res, nil
	}

	ips, err := r.LookupHost(ctx, name)
	if err != nil {
		return res, err
	}
	res.IPs = ips
	if cname, err := r.LookupCNAME(ctx, name); err == nil && cname != name && cname != name+"." {
		res.CNAME = cname
	}
	return res, nil
}
