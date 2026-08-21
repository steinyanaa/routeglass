package grant

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Claims struct {
	Version          int      `json:"v"`
	KeyID            string   `json:"kid"`
	NodeID           string   `json:"node"`
	SessionID        string   `json:"session"`
	TestID           string   `json:"test"`
	ClientIP         string   `json:"client_ip"`
	Scopes           []string `json:"scope"`
	MaxDownloadBytes int64    `json:"max_download_bytes"`
	MaxUploadBytes   int64    `json:"max_upload_bytes"`
	MaxStreams       int      `json:"max_streams"`
	NotBefore        int64    `json:"nbf"`
	Expires          int64    `json:"exp"`
	Nonce            string   `json:"nonce"`
}

var ErrInvalid = errors.New("invalid test grant")

func Sign(private ed25519.PrivateKey, c Claims) (string, error) {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Nonce == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		c.Nonce = base64.RawURLEncoding.EncodeToString(b)
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(b)
	sig := ed25519.Sign(private, []byte("rgtt1."+p))
	return "rgtt1." + p + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func Verify(public ed25519.PublicKey, token string, now time.Time) (Claims, error) {
	var c Claims
	p := strings.Split(token, ".")
	if len(p) != 3 || p[0] != "rgtt1" {
		return c, ErrInvalid
	}
	b, e1 := base64.RawURLEncoding.DecodeString(p[1])
	sig, e2 := base64.RawURLEncoding.DecodeString(p[2])
	if e1 != nil || e2 != nil || !ed25519.Verify(public, []byte("rgtt1."+p[1]), sig) || json.Unmarshal(b, &c) != nil {
		return c, ErrInvalid
	}
	if c.Version != 1 || c.NotBefore > now.Unix() || c.Expires < now.Unix() || c.Nonce == "" {
		return c, ErrInvalid
	}
	return c, nil
}

func HasScope(c Claims, scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}
