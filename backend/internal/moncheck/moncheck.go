// Package moncheck runs a single custom-monitor probe: TCP connect or HTTP(S) GET
// (Doc 3 §9). It is deliberately stateless — the scheduler owns debounce/persistence.
package moncheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Spec is one check to run. Host is the device's resolved IP (probes always target
// the IP, never the hostname — same rule as the ping loop).
type Spec struct {
	Kind        string // "TCP" | "HTTP" | "HTTPS"
	Host        string
	Port        int // 0 => kind default (HTTP 80, HTTPS 443)
	Path        string
	URLOverride string // absolute URL; wins over Host/Port/Path
	ExpectLo    int
	ExpectHi    int
	Timeout     time.Duration
}

// Result is what one probe observed. HTTPStatus/CertExpiresAt are nil for TCP.
type Result struct {
	OK            bool
	DurationMs    float64
	HTTPStatus    *int
	CertExpiresAt *time.Time
}

func Check(ctx context.Context, s Spec) Result {
	if s.Kind == "TCP" {
		return checkTCP(ctx, s)
	}
	return checkHTTP(ctx, s)
}

func checkTCP(ctx context.Context, s Spec) Result {
	start := time.Now()
	d := net.Dialer{Timeout: s.Timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)))
	dur := msSince(start)
	if err != nil {
		return Result{OK: false, DurationMs: dur}
	}
	_ = conn.Close()
	return Result{OK: true, DurationMs: dur}
}

func checkHTTP(ctx context.Context, s Spec) Result {
	url := buildURL(s)
	// InsecureSkipVerify: a health check on an internal host often has a self-signed
	// or IP-mismatched cert; we care about reachability + status + the cert's expiry,
	// not its trust chain. A stricter "cert valid" mode is a later refinement.
	client := &http.Client{
		Timeout:   s.Timeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{OK: false, DurationMs: msSince(start)}
	}
	resp, err := client.Do(req)
	dur := msSince(start)
	if err != nil {
		return Result{OK: false, DurationMs: dur}
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	res := Result{DurationMs: dur, HTTPStatus: &status, OK: status >= s.ExpectLo && status <= s.ExpectHi}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		exp := resp.TLS.PeerCertificates[0].NotAfter
		res.CertExpiresAt = &exp
	}
	return res
}

// buildURL assembles the request URL. An absolute override wins; otherwise it is
// scheme://host[:port]path, with the default port for the scheme omitted.
func buildURL(s Spec) string {
	if s.URLOverride != "" {
		return s.URLOverride
	}
	scheme, def := "http", 80
	if s.Kind == "HTTPS" {
		scheme, def = "https", 443
	}
	port := s.Port
	if port == 0 {
		port = def
	}
	host := s.Host
	if port != def {
		host = net.JoinHostPort(s.Host, strconv.Itoa(port))
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, s.Path)
}

func msSince(t time.Time) float64 { return float64(time.Since(t).Microseconds()) / 1000.0 }
