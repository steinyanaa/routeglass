package routeexec

import (
	"context"
	"testing"
)

func TestRejectsNonIPTargets(t *testing.T) {
	for _, target := range []string{"example.com", "127.0.0.1;id", "$(id)", ""} {
		if _, _, err := Run(context.Background(), target); err == nil {
			t.Fatalf("accepted unsafe target %q", target)
		}
	}
}

func TestParseHops(t *testing.T) {
	hops := parseHops([]byte(" 1  192.0.2.1  1.25 ms\n 2  2001:db8::1  8.50 ms\n"))
	if len(hops) != 2 || hops[0].Address != "192.0.2.1" || hops[1].Address != "2001:db8::1" {
		t.Fatalf("unexpected hops: %#v", hops)
	}
}
