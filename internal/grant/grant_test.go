package grant

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestGrantRoundTripAndTamper(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	tok, e := Sign(priv, Claims{NodeID: "n", Scopes: []string{"probe"}, NotBefore: now.Add(-time.Second).Unix(), Expires: now.Add(time.Minute).Unix()})
	if e != nil {
		t.Fatal(e)
	}
	c, e := Verify(pub, tok, now)
	if e != nil || c.NodeID != "n" || !HasScope(c, "probe") {
		t.Fatal(c, e)
	}
	replacement := "A"
	if tok[len(tok)-1] == 'A' {
		replacement = "B"
	}
	tok = tok[:len(tok)-1] + replacement
	if _, e = Verify(pub, tok, now); e == nil {
		t.Fatal("tamper accepted")
	}
}
func TestGrantExpiry(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	tok, _ := Sign(priv, Claims{NotBefore: now.Add(-time.Minute).Unix(), Expires: now.Add(-time.Second).Unix()})
	if _, e := Verify(pub, tok, now); e == nil {
		t.Fatal("expired accepted")
	}
}
