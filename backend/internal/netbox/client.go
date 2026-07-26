// Package netbox is a minimal read-only Netbox API client: enough to snapshot IP
// addresses for the sync worker (Doc 3 §3). No Netbox SDK — stdlib net/http.
package netbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// IPObject is the slice of a Netbox ip-address record EchoMap cares about.
type IPObject struct {
	NetboxID int64
	IP       string // address with the /mask stripped
	Name     string
	Site     string
}

type Client struct {
	url   string
	token string
	http  *http.Client
}

func New(url, token string) *Client {
	return &Client{
		url:   strings.TrimRight(strings.TrimSpace(url), "/"),
		token: strings.TrimSpace(token),
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Configured() bool { return c.url != "" && c.token != "" }

// nbPage is one page of GET /api/ipam/ip-addresses/.
type nbPage struct {
	Next    string `json:"next"`
	Results []struct {
		ID             int64  `json:"id"`
		Address        string `json:"address"` // "10.10.30.9/24"
		DNSName        string `json:"dns_name"`
		AssignedObject *struct {
			Device *struct {
				Name string `json:"name"`
				Site *struct {
					Name string `json:"name"`
				} `json:"site"`
			} `json:"device"`
		} `json:"assigned_object"`
	} `json:"results"`
}

// FetchAllIPs pages through every IP address in Netbox. name falls back
// dns_name → assigned device name → IP; site comes from the assigned device.
func (c *Client) FetchAllIPs(ctx context.Context) ([]IPObject, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("netbox not configured")
	}
	url := c.url + "/api/ipam/ip-addresses/?limit=200"
	var out []IPObject
	for page := 0; url != "" && page < 100; page++ { // 100-page guard (20k IPs)
		var body nbPage
		if err := c.getJSON(ctx, url, &body); err != nil {
			return nil, err
		}
		for _, r := range body.Results {
			ip := r.Address
			if i := strings.IndexByte(ip, '/'); i >= 0 {
				ip = ip[:i]
			}
			if ip == "" {
				continue
			}
			obj := IPObject{NetboxID: r.ID, IP: ip, Name: r.DNSName}
			if r.AssignedObject != nil && r.AssignedObject.Device != nil {
				if obj.Name == "" {
					obj.Name = r.AssignedObject.Device.Name
				}
				if r.AssignedObject.Device.Site != nil {
					obj.Site = r.AssignedObject.Device.Site.Name
				}
			}
			if obj.Name == "" {
				obj.Name = ip
			}
			out = append(out, obj)
		}
		url = body.Next // Netbox returns an absolute URL or "" on the last page
	}
	return out, nil
}

// Test verifies URL + token by hitting the status endpoint.
func (c *Client) Test(ctx context.Context) error {
	if !c.Configured() {
		return fmt.Errorf("netbox url and token are required")
	}
	var discard map[string]any
	return c.getJSON(ctx, c.url+"/api/status/", &discard)
}

func (c *Client) getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("netbox %s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
