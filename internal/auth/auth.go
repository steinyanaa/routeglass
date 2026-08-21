package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const passwordPrefix = "argon2id$v=19$m=65536,t=3,p="

func RandomToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(token string) []byte { h := sha256.Sum256([]byte(token)); return h[:] }

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	p := uint8(4)
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, p, 32)
	return fmt.Sprintf("%s%d$%s$%s", passwordPrefix, p, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" || parts[2] != "m=65536,t=3,p=4" {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[3])
	want, e2 := base64.RawStdEncoding.DecodeString(parts[4])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

func AccessCode(secret []byte, generation uint64, at time.Time, window time.Duration) string {
	if window <= 0 {
		window = 10 * time.Minute
	}
	b := make([]byte, 16)
	binary.BigEndian.PutUint64(b[:8], generation)
	binary.BigEndian.PutUint64(b[8:], uint64(at.Unix()/int64(window/time.Second)))
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("routeglass/access/v1"))
	m.Write(b)
	n := binary.BigEndian.Uint32(m.Sum(nil)[:4]) % 1_000_000
	return fmt.Sprintf("%06d", n)
}

func VerifyAccessCode(secret []byte, generation uint64, code string, at time.Time, window, grace time.Duration) bool {
	if len(code) != 6 {
		return false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return false
	}
	if hmac.Equal([]byte(code), []byte(AccessCode(secret, generation, at, window))) {
		return true
	}
	boundary := time.Unix((at.Unix()/int64(window/time.Second))*int64(window/time.Second), 0)
	return at.Sub(boundary) <= grace && hmac.Equal([]byte(code), []byte(AccessCode(secret, generation, at.Add(-window), window)))
}

var ErrInvalidCSRF = errors.New("invalid csrf token")

func VerifyCSRF(got, want string) error {
	if got == "" || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return ErrInvalidCSRF
	}
	return nil
}
