package server

import (
	"bytes"
	"encoding/json"
	"github.com/steinyanaa/routeglass/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, e := New(Config{DataDir: t.TempDir(), PublicOrigin: "http://example.test", InsecureCookies: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestHealthBootstrapAndEmbeddedUI(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/healthz", "/api/v1/bootstrap", "/deep/spa/route"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
func TestInviteConsumedOnceAndCSRFExposed(t *testing.T) {
	s := newTestServer(t)
	tok := "invite-token"
	s.store.DB.Exec(`INSERT INTO invites(id,token_hash,expires_at,created_at) VALUES('i',?,?,?)`, auth.HashToken(tok), time.Now().Add(time.Hour).Unix(), time.Now().Unix())
	body, _ := json.Marshal(map[string]string{"token": tok})
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/access/invite", bytes.NewReader(body))
		r.RemoteAddr = "192.0.2.5:80"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := request()
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CSRF") && !strings.Contains(strings.ToLower(w.Body.String()), "csrf") {
		t.Fatal("csrf missing")
	}
	if w2 := request(); w2.Code != 401 {
		t.Fatalf("replay=%d", w2.Code)
	}
}
func TestSecureCookieName(t *testing.T) {
	s := newTestServer(t)
	s.cfg.InsecureCookies = false
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.0.2.1:4"
	if _, e := s.newAccess(w, r); e != nil {
		t.Fatal(e)
	}
	c := w.Result().Cookies()[0]
	if c.Name != "__Host-rg_access" || !c.Secure || c.Path != "/" {
		t.Fatal(c)
	}
}
func TestErrorShape(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest("POST", "/api/v1/tests", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	var e apiError
	if json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Code != "access_required" || e.RequestID == "" {
		t.Fatal(w.Body.String())
	}
}

var _ = http.MethodGet
