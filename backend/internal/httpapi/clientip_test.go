package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestNewClientIP(t *testing.T) {
	caddy := []netip.Prefix{netip.MustParsePrefix("172.30.0.10/32")}
	docNet := []netip.Prefix{netip.MustParsePrefix("2001:db8::/32")}

	cases := []struct {
		name       string
		trusted    []netip.Prefix
		remoteAddr string
		xff        []string // one element per header line
		want       string
	}{
		// trusted == nil is the pre-proxy behavior, byte for byte.
		{"no proxy: ipv4 with port", nil, "192.0.2.1:54321", nil, "192.0.2.1"},
		{"no proxy: ipv6 with port", nil, "[2001:db8::1]:443", nil, "2001:db8::1"},
		{"no proxy: x-forwarded-for is ignored", nil, "192.0.2.1:54321", []string{"1.2.3.4"}, "192.0.2.1"},
		{"no proxy: unsplittable remote addr returned as-is", nil, "not-an-addr", []string{"203.0.113.9"}, "not-an-addr"},

		{"untrusted peer, no header", caddy, "198.51.100.7:4000", nil, "198.51.100.7"},
		{"untrusted peer, header ignored", caddy, "198.51.100.7:4000", []string{"203.0.113.9"}, "198.51.100.7"},
		{"trusted peer, no header", caddy, "172.30.0.10:4000", nil, "172.30.0.10"},
		{"trusted peer, single entry", caddy, "172.30.0.10:4000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"client-supplied prefix ignored", caddy, "172.30.0.10:4000", []string{"1.1.1.1, 203.0.113.9"}, "203.0.113.9"},
		{"header lines joined in order", caddy, "172.30.0.10:4000", []string{"1.1.1.1", "203.0.113.9"}, "203.0.113.9"},
		{"trusted entries skipped from the right", caddy, "172.30.0.10:4000", []string{"203.0.113.9, 172.30.0.10"}, "203.0.113.9"},
		{"every entry trusted falls back to peer", caddy, "172.30.0.10:4000", []string{"172.30.0.10"}, "172.30.0.10"},
		{"unparseable entry fails closed to peer", caddy, "172.30.0.10:4000", []string{"203.0.113.9, garbage"}, "172.30.0.10"},
		{"ipv4-mapped peer is unmapped before matching", caddy, "[::ffff:172.30.0.10]:4000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"ipv4-mapped entry is returned unmapped", caddy, "172.30.0.10:4000", []string{"::ffff:203.0.113.9"}, "203.0.113.9"},
		{"ipv6: entries inside the trusted prefix are all trusted", docNet, "[2001:db8::1]:4000", []string{"2001:db8:ffff::5, 2001:db8::1"}, "2001:db8::1"},
		{"ipv6: rightmost untrusted", docNet, "[2001:db8::1]:4000", []string{"2001:db9::5, 2001:db8::1"}, "2001:db9::5"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}

			if got := NewClientIP(tt.trusted)(req); got != tt.want {
				t.Errorf("NewClientIP(%v)(RemoteAddr=%q, XFF=%q) = %q, want %q", tt.trusted, tt.remoteAddr, tt.xff, got, tt.want)
			}
		})
	}
}
