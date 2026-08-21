package netutil

import (
	"net/http"
	"net/netip"
	"strings"
)

func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer, _ := netip.ParseAddrPort(r.RemoteAddr)
	ip := peer.Addr().Unmap()
	if !contains(trusted, ip) {
		return ip
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	chain := make([]netip.Addr, 0, len(parts)+1)
	for _, p := range parts {
		if a, e := netip.ParseAddr(strings.TrimSpace(p)); e == nil {
			chain = append(chain, a.Unmap())
		}
	}
	chain = append(chain, ip)
	for i := len(chain) - 1; i >= 0; i-- {
		if !contains(trusted, chain[i]) {
			return chain[i]
		}
	}
	return chain[0]
}
func contains(ps []netip.Prefix, a netip.Addr) bool {
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
