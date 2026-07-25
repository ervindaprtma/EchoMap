package moncheck

import "testing"

func TestBuildURL(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{"http default port omitted", Spec{Kind: "HTTP", Host: "10.0.0.5", Path: "/health"}, "http://10.0.0.5/health"},
		{"https default port omitted", Spec{Kind: "HTTPS", Host: "10.0.0.5"}, "https://10.0.0.5"},
		{"http custom port", Spec{Kind: "HTTP", Host: "10.0.0.5", Port: 8080, Path: "/admin"}, "http://10.0.0.5:8080/admin"},
		{"https custom port", Spec{Kind: "HTTPS", Host: "10.0.0.5", Port: 8443}, "https://10.0.0.5:8443"},
		{"override wins", Spec{Kind: "HTTP", Host: "10.0.0.5", Port: 80, URLOverride: "https://api.example.com/x"}, "https://api.example.com/x"},
	}
	for _, c := range cases {
		if got := buildURL(c.spec); got != c.want {
			t.Errorf("%s: buildURL = %q, want %q", c.name, got, c.want)
		}
	}
}
