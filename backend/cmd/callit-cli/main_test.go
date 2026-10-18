package main

import "testing"

func TestEndpoints(t *testing.T) {
	cases := []struct {
		name             string
		addr             string
		secure           bool
		wantHTTP, wantWS string
	}{
		{"plain local server", "localhost:8080", false, "http://localhost:8080", "ws://localhost:8080"},
		{"tls behind a proxy", "callit.duckdns.org", true, "https://callit.duckdns.org", "wss://callit.duckdns.org"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			gotHTTP, gotWS := endpoints(tt.addr, tt.secure)
			if gotHTTP != tt.wantHTTP || gotWS != tt.wantWS {
				t.Errorf("endpoints(%q, %v) = (%q, %q), want (%q, %q)", tt.addr, tt.secure, gotHTTP, gotWS, tt.wantHTTP, tt.wantWS)
			}
		})
	}
}
