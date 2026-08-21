package auth

import (
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, e := HashPassword("correct horse battery staple")
	if e != nil {
		t.Fatal(e)
	}
	if !VerifyPassword(h, "correct horse battery staple") {
		t.Fatal("password rejected")
	}
	if VerifyPassword(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}
func TestAccessCodeBoundaryAndRotate(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	at := time.Unix(1201, 0)
	c := AccessCode(secret, 7, time.Unix(1199, 0), 10*time.Minute)
	if !VerifyAccessCode(secret, 7, c, at, 10*time.Minute, 30*time.Second) {
		t.Fatal("previous code should be in grace")
	}
	if VerifyAccessCode(secret, 8, c, at, 10*time.Minute, 30*time.Second) {
		t.Fatal("rotated generation accepted")
	}
	if VerifyAccessCode(secret, 7, c, time.Unix(1240, 0), 10*time.Minute, 30*time.Second) {
		t.Fatal("expired grace accepted")
	}
}
func TestTokenHashStable(t *testing.T) {
	a := HashToken("a")
	b := HashToken("a")
	c := HashToken("b")
	if string(a) != string(b) || string(a) == string(c) {
		t.Fatal("token hash")
	}
}
