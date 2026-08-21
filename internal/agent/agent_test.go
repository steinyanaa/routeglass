package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/steinyanaa/routeglass/internal/grant"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestJoinUsesSnakeCaseContract(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	serverPub, _, _ := ed25519.GenerateKey(rand.Reader)
	var received map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"node_id":           "node-1",
			"server_public_key": base64.RawStdEncoding.EncodeToString(serverPub),
		})
	}))
	defer ts.Close()
	a := &Agent{
		cfg:     Config{ServerURL: ts.URL, JoinToken: "rgj_test", NodeID: "node-1", Name: "Test", Endpoint: "https://node.test:9443"},
		cfgPath: filepath.Join(t.TempDir(), "agent.json"),
		priv:    priv,
	}
	if err := a.join(context.Background()); err != nil {
		t.Fatal(err)
	}
	if received["token"] != "rgj_test" || received["node_id"] != "node-1" || received["public_key"] != base64.RawStdEncoding.EncodeToString(pub) {
		t.Fatalf("unexpected join payload: %#v", received)
	}
}

func TestDownloadAllowsBrowserEncodingAndRejectsRange(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	a := &Agent{cfg: Config{NodeID: "n", AllowedOrigin: "https://ui.test"}, serverPub: pub, ledger: &ledger{byNonce: map[string]*usage{}, limit: 10000}}
	a.tlsReady.Store(true)
	now := time.Now()
	tok, _ := grant.Sign(priv, grant.Claims{NodeID: "n", ClientIP: "192.0.2.1", Scopes: []string{"download"}, MaxDownloadBytes: 1000, MaxStreams: 2, NotBefore: now.Add(-time.Second).Unix(), Expires: now.Add(time.Minute).Unix()})
	req := httptest.NewRequest("GET", "https://n/agent/v1/download?bytes=128", nil)
	req.RemoteAddr = "192.0.2.1:123"
	req.Header.Set("Origin", "https://ui.test")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept-Encoding", "gzip, br")
	w := httptest.NewRecorder()
	a.download(w, req)
	if w.Code != 200 || w.Body.Len() != 128 || w.Header().Get("Content-Encoding") != "identity" {
		t.Fatal(w.Code, w.Body.Len(), w.Header())
	}
	req = httptest.NewRequest("GET", "https://n/agent/v1/download?bytes="+strconv.Itoa(128), nil)
	req.RemoteAddr = "192.0.2.1:123"
	req.Header.Set("Origin", "https://ui.test")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Range", "bytes=0-10")
	w = httptest.NewRecorder()
	a.download(w, req)
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatal(w.Code)
	}
}
func TestMissingOriginDenied(t *testing.T) {
	a := &Agent{cfg: Config{AllowedOrigin: "https://ui.test"}, ledger: &ledger{byNonce: map[string]*usage{}}}
	a.tlsReady.Store(true)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "https://n/agent/v1/probe", nil)
	a.preflight(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestAuthorizeIPv6RemoteAddress(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	a := &Agent{cfg: Config{NodeID: "n", AllowedOrigin: "https://ui.test"}, serverPub: pub, ledger: &ledger{byNonce: map[string]*usage{}, limit: 10000}}
	a.tlsReady.Store(true)
	now := time.Now()
	tok, _ := grant.Sign(priv, grant.Claims{NodeID: "n", ClientIP: "2001:db8::1", Scopes: []string{"probe"}, NotBefore: now.Add(-time.Second).Unix(), Expires: now.Add(time.Minute).Unix()})
	req := httptest.NewRequest("GET", "https://n/agent/v1/probe", nil)
	req.RemoteAddr = "[2001:db8::1]:1234"
	req.Header.Set("Origin", "https://ui.test")
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	a.probe(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("IPv6 peer rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestConcurrentTestLimit(t *testing.T) {
	a := &Agent{ledger: &ledger{byNonce: map[string]*usage{}, active: map[string]int{}, limit: 1 << 20, maxActive: 3}}
	releases := make([]func(), 0, 3)
	for i := 0; i < 3; i++ {
		release, ok := a.reserve(grant.Claims{TestID: "test-" + strconv.Itoa(i), Nonce: "nonce-" + strconv.Itoa(i), MaxStreams: 1, MaxDownloadBytes: 100}, 10, 0)
		if !ok {
			t.Fatalf("reservation %d rejected", i)
		}
		releases = append(releases, release)
	}
	if _, ok := a.reserve(grant.Claims{TestID: "test-4", Nonce: "nonce-4", MaxStreams: 1, MaxDownloadBytes: 100}, 10, 0); ok {
		t.Fatal("fourth concurrent test accepted")
	}
	releases[0]()
	if release, ok := a.reserve(grant.Claims{TestID: "test-4", Nonce: "nonce-4", MaxStreams: 1, MaxDownloadBytes: 100}, 10, 0); !ok {
		t.Fatal("reservation remained blocked after a test completed")
	} else {
		release()
	}
	for _, release := range releases[1:] {
		release()
	}
}

func TestDailyQuotaPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota.json")
	a := &Agent{ledger: &ledger{byNonce: map[string]*usage{}, active: map[string]int{}, limit: 1000, maxActive: 3, path: path}}
	release, ok := a.reserve(grant.Claims{TestID: "test", Nonce: "nonce", MaxStreams: 1, MaxDownloadBytes: 1000}, 400, 0)
	if !ok {
		t.Fatal("reservation rejected")
	}
	release()
	restored := &ledger{path: path}
	restored.load()
	if restored.daily != 400 || restored.day != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("quota not restored: %#v", restored)
	}
}
