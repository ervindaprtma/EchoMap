// Package ping sends ICMP echo probes.
package ping

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

var seqCounter uint32

// ErrNoIPv4 means the name resolved, but to nothing this package can probe.
var ErrNoIPv4 = errors.New("hostname has no IPv4 address")

const resolveTimeout = 3 * time.Second

// Resolve4 looks up host and returns its first IPv4 address in normalized form.
// IPv4-only because Ping speaks ip4:icmp: an AAAA-only answer is unpingable, so
// callers must treat it as a failed lookup rather than store it. Every hostname
// that reaches a device row goes through here (API create + resolver loop).
func Resolve4(ctx context.Context, host string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		// Unmap first so a v4-mapped answer (::ffff:10.0.0.5) stores as plain v4.
		if ip, err := netip.ParseAddr(a); err == nil && ip.Unmap().Is4() {
			return ip.Unmap().String(), nil
		}
	}
	return "", ErrNoIPv4
}

// Ping sends one ICMP echo to addr and waits up to timeout for the reply.
// alive=false with err=nil means "no reply" (a normal DOWN), not a failure.
// A non-nil err means the probe couldn't be sent (e.g. missing CAP_NET_RAW).
//
// ponytail: one raw socket per call. A raw ICMP socket receives every host reply,
// so we filter by peer + echo id/seq. Fine at ~500 devices / concurrency 32;
// upgrade to a single shared socket with an id->waiter demux if that ceiling bites.
func Ping(addr string, timeout time.Duration) (alive bool, rttMs float64, err error) {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false, 0, err
	}
	defer conn.Close()

	dst, err := net.ResolveIPAddr("ip4", addr)
	if err != nil {
		return false, 0, err
	}

	id := os.Getpid() & 0xffff
	seq := int(atomic.AddUint32(&seqCounter, 1) & 0xffff)
	wb, err := (&icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("echomap-ping")},
	}).Marshal(nil)
	if err != nil {
		return false, 0, err
	}

	start := time.Now()
	if _, err := conn.WriteTo(wb, dst); err != nil {
		return false, 0, err
	}

	deadline := start.Add(timeout)
	_ = conn.SetReadDeadline(deadline)
	rb := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(rb)
		if err != nil {
			return false, 0, nil // timeout: no reply => DOWN, not an error
		}
		if peer.String() != dst.IP.String() {
			continue
		}
		rm, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), rb[:n])
		if err != nil {
			continue
		}
		if echo, ok := rm.Body.(*icmp.Echo); ok &&
			rm.Type == ipv4.ICMPTypeEchoReply && echo.ID == id && echo.Seq == seq {
			return true, float64(time.Since(start).Microseconds()) / 1000.0, nil
		}
		if time.Now().After(deadline) {
			return false, 0, nil
		}
	}
}
