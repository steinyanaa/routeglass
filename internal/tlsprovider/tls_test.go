package tlsprovider

import "testing"

func TestACMEIPDefaultsShortLived(t *testing.T) {
	a, e := NewACME(ACMEConfig{Mode: "acme-ip", Names: []string{"192.0.2.1"}})
	if e != nil {
		t.Fatal(e)
	}
	if a.cfg.Profile != "shortlived" {
		t.Fatal(a.cfg.Profile)
	}
	if _, e = NewACME(ACMEConfig{Mode: "acme-ip", Names: []string{"example.com"}}); e == nil {
		t.Fatal("domain accepted in IP mode")
	}
}
func TestACMEModeValidation(t *testing.T) {
	if _, e := NewACME(ACMEConfig{Mode: "bad", Names: []string{"example.com"}}); e == nil {
		t.Fatal("bad mode accepted")
	}
}
