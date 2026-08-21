package netutil

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIPTrustedChain(t *testing.T) {
	r := httptest.NewRequest("GET", "http://x", nil)
	r.RemoteAddr = "10.0.0.2:123"
	r.Header.Set("X-Forwarded-For", "198.51.100.8, 10.0.0.1")
	got := ClientIP(r, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	if got.String() != "198.51.100.8" {
		t.Fatal(got)
	}
}
func TestClientIPIgnoresSpoofedHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "http://x", nil)
	r.RemoteAddr = "203.0.113.9:123"
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	if got := ClientIP(r, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}); got.String() != "203.0.113.9" {
		t.Fatal(got)
	}
}
func TestMappedIPv6(t *testing.T) {
	r := httptest.NewRequest("GET", "http://x", nil)
	r.RemoteAddr = "[::ffff:192.0.2.4]:123"
	if got := ClientIP(r, nil); got.String() != "192.0.2.4" {
		t.Fatal(got)
	}
}
