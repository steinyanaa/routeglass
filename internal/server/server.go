package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/steinyanaa/routeglass/internal/auth"
	"github.com/steinyanaa/routeglass/internal/grant"
	"github.com/steinyanaa/routeglass/internal/netutil"
	"github.com/steinyanaa/routeglass/internal/score"
	"github.com/steinyanaa/routeglass/internal/store"
)

type Config struct {
	Listen          string   `json:"listen"`
	DataDir         string   `json:"data_dir"`
	PublicOrigin    string   `json:"public_origin"`
	TrustedProxies  []string `json:"trusted_proxies"`
	InsecureCookies bool     `json:"insecure_cookies"`
	WebDir          string   `json:"web_dir"`
}

//go:embed webdist/*
var embeddedWeb embed.FS

type secrets struct {
	AccessSecret string `json:"access_secret"`
	PrivateKey   string `json:"private_key"`
	PublicKey    string `json:"public_key"`
	KeyID        string `json:"key_id"`
}
type Server struct {
	cfg      Config
	store    *store.Store
	sec      secrets
	priv     ed25519.PrivateKey
	pub      ed25519.PublicKey
	trusted  []netip.Prefix
	mux      *http.ServeMux
	controls sync.Map
	limitMu  sync.Mutex
	limits   map[string]attemptBucket
}

type attemptBucket struct {
	Started time.Time
	Count   int
}

func LoadConfig(path string) (Config, error) {
	c := Config{Listen: "127.0.0.1:8765", DataDir: "./data", PublicOrigin: "http://127.0.0.1:8765", InsecureCookies: true}
	if path == "" {
		return c, nil
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}

func New(cfg Config) (*Server, error) {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8765"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, err
	}
	st, e := store.Open(filepath.Join(cfg.DataDir, "routeglass.db"))
	if e != nil {
		return nil, e
	}
	sec, priv, pub, e := loadSecrets(filepath.Join(cfg.DataDir, "secrets.json"))
	if e != nil {
		st.Close()
		return nil, e
	}
	s := &Server{cfg: cfg, store: st, sec: sec, priv: priv, pub: pub, mux: http.NewServeMux(), limits: make(map[string]attemptBucket)}
	for _, v := range cfg.TrustedProxies {
		p, e := netip.ParsePrefix(v)
		if e != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", v, e)
		}
		s.trusted = append(s.trusted, p)
	}
	if e = s.ensureAdmin(context.Background()); e != nil {
		st.Close()
		return nil, e
	}
	s.routes()
	return s, nil
}
func (s *Server) Close() error          { return s.store.Close() }
func (s *Server) Handler() http.Handler { return requestID(s.mux) }
func (s *Server) ListenAndServe(ctx context.Context) error {
	h := &http.Server{Addr: s.cfg.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		h.Shutdown(c)
	}()
	e := h.ListenAndServe()
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

func loadSecrets(path string) (secrets, ed25519.PrivateKey, ed25519.PublicKey, error) {
	var s secrets
	b, e := os.ReadFile(path)
	if e == nil {
		if e = json.Unmarshal(b, &s); e != nil {
			return s, nil, nil, e
		}
		pr, e := base64.RawStdEncoding.DecodeString(s.PrivateKey)
		if e != nil {
			return s, nil, nil, e
		}
		pu, e := base64.RawStdEncoding.DecodeString(s.PublicKey)
		return s, ed25519.PrivateKey(pr), ed25519.PublicKey(pu), e
	}
	if !os.IsNotExist(e) {
		return s, nil, nil, e
	}
	as, e := auth.RandomToken(32)
	if e != nil {
		return s, nil, nil, e
	}
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return s, nil, nil, e
	}
	kidRaw := sha256.Sum256(pub)
	s = secrets{AccessSecret: as, PrivateKey: base64.RawStdEncoding.EncodeToString(priv), PublicKey: base64.RawStdEncoding.EncodeToString(pub), KeyID: base64.RawURLEncoding.EncodeToString(kidRaw[:8])}
	b, _ = json.MarshalIndent(s, "", "  ")
	if e = os.WriteFile(path, b, 0600); e != nil {
		return s, nil, nil, e
	}
	return s, priv, pub, nil
}
func (s *Server) ensureAdmin(ctx context.Context) error {
	var n int
	if e := s.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM admins`).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	pw, e := auth.RandomToken(24)
	if e != nil {
		return e
	}
	h, e := auth.HashPassword(pw)
	if e != nil {
		return e
	}
	passwordPath := filepath.Join(s.cfg.DataDir, "initial-admin-password")
	tmpPath := passwordPath + ".tmp"
	if e = os.WriteFile(tmpPath, []byte(pw+"\n"), 0600); e != nil {
		return e
	}
	if e = os.Rename(tmpPath, passwordPath); e != nil {
		_ = os.Remove(tmpPath)
		return e
	}
	_, e = s.store.DB.ExecContext(ctx, `INSERT INTO admins(id,username,password_hash,must_change,created_at) VALUES('admin','admin',?,1,?)`, h, time.Now().Unix())
	if e == nil {
		slog.Warn("temporary admin password created", "path", passwordPath)
	} else {
		_ = os.Remove(passwordPath)
	}
	return e
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, map[string]any{"status": "ok"}) })
	s.mux.HandleFunc("GET /readyz", s.ready)
	s.mux.HandleFunc("GET /install/agent.sh", s.agentInstaller)
	s.mux.HandleFunc("GET /api/v1/bootstrap", s.bootstrap)
	s.mux.HandleFunc("GET /api/v1/network", s.network)
	s.mux.HandleFunc("POST /api/v1/access/code", s.accessCode)
	s.mux.HandleFunc("POST /api/v1/access/invite", s.accessInvite)
	s.mux.HandleFunc("GET /api/v1/access/session", s.accessSession)
	s.mux.HandleFunc("DELETE /api/v1/access/session", s.accessLogout)
	s.mux.HandleFunc("POST /api/v1/tests", s.createTest)
	s.mux.HandleFunc("GET /api/v1/tests/{id}", s.getTest)
	s.mux.HandleFunc("POST /api/v1/tests/{id}/probes", s.probes)
	s.mux.HandleFunc("POST /api/v1/tests/{id}/grants", s.grants)
	s.mux.HandleFunc("POST /api/v1/tests/{id}/results", s.results)
	s.mux.HandleFunc("POST /api/v1/tests/{id}/route", s.route)
	s.mux.HandleFunc("GET /api/v1/tests/{id}/events", s.events)
	s.mux.HandleFunc("POST /api/v1/agent/join", s.join)
	s.mux.HandleFunc("GET /api/v1/agent/control", s.control)
	s.mux.HandleFunc("POST /api/v1/admin/login", s.adminLogin)
	s.mux.HandleFunc("POST /api/v1/admin/password", s.adminPassword)
	s.mux.HandleFunc("POST /api/v1/admin/logout", s.adminLogout)
	s.mux.HandleFunc("GET /api/v1/admin/overview", s.adminOverview)
	s.mux.HandleFunc("GET /api/v1/admin/access", s.adminAccess)
	s.mux.HandleFunc("POST /api/v1/admin/access", s.rotateAccess)
	s.mux.HandleFunc("GET /api/v1/admin/invites", s.listInvites)
	s.mux.HandleFunc("POST /api/v1/admin/invites", s.createInvite)
	s.mux.HandleFunc("GET /api/v1/admin/nodes", s.adminNodes)
	s.mux.HandleFunc("POST /api/v1/admin/nodes", s.createJoin)
	s.mux.HandleFunc("PUT /api/v1/admin/nodes/{id}", s.updateNode)
	s.mux.HandleFunc("DELETE /api/v1/admin/nodes/{id}", s.deleteNode)
	s.mux.HandleFunc("GET /api/v1/admin/sessions", s.adminSessions)
	s.mux.HandleFunc("DELETE /api/v1/admin/sessions/{id}", s.revokeSession)
	s.mux.HandleFunc("GET /api/v1/admin/settings", s.adminSettings)
	s.mux.HandleFunc("PUT /api/v1/admin/settings", s.adminSettings)
	if s.cfg.WebDir != "" {
		files := http.FileServer(http.Dir(s.cfg.WebDir))
		s.mux.Handle("/", files)
	} else {
		sub, _ := fs.Sub(embeddedWeb, "webdist")
		s.mux.Handle("/", spaHandler(sub))
	}
}
func spaHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, e := fs.Stat(fsys, p); e != nil {
			b, readErr := fs.ReadFile(fsys, "index.html")
			if readErr != nil {
				http.Error(w, "UI unavailable", 503)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
			return
		}
		if p == "index.html" {
			b, _ := fs.ReadFile(fsys, p)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + p
		http.FileServer(http.FS(fsys)).ServeHTTP(w, r2)
	})
}

type apiError struct {
	Code      string         `json:"code"`
	Params    map[string]any `json:"params"`
	Retry     int            `json:"retry_after_seconds"`
	RequestID string         `json:"request_id"`
}

func fail(w http.ResponseWriter, r *http.Request, status int, code string) {
	jsonOut(w, status, apiError{Code: code, Params: map[string]any{}, RequestID: r.Header.Get("X-Request-ID")})
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.RandomToken(12)
		r.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowAttempt(key string, maximum int, window time.Duration) bool {
	now := time.Now()
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	if len(s.limits) > 4096 {
		for k, candidate := range s.limits {
			if now.Sub(candidate.Started) >= window {
				delete(s.limits, k)
			}
		}
	}
	b := s.limits[key]
	if b.Started.IsZero() || now.Sub(b.Started) >= window {
		b = attemptBucket{Started: now}
	}
	if b.Count >= maximum {
		return false
	}
	b.Count++
	s.limits[key] = b
	return true
}

func (s *Server) clearAttempts(key string) {
	s.limitMu.Lock()
	delete(s.limits, key)
	s.limitMu.Unlock()
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if e := s.store.DB.PingContext(r.Context()); e != nil {
		fail(w, r, 503, "database_unavailable")
		return
	}
	jsonOut(w, 200, map[string]string{"status": "ready"})
}
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	a := s.getAccess(r)
	csrf := ""
	if a != nil {
		csrf = a.CSRF
	}
	jsonOut(w, 200, map[string]any{"name": "RouteGlass", "version": "v0.1.0", "public_key": s.sec.PublicKey, "key_id": s.sec.KeyID, "authenticated": a != nil, "csrf": csrf})
}
func (s *Server) agentInstaller(w http.ResponseWriter, r *http.Request) {
	paths := []string{os.Getenv("ROUTEGLASS_AGENT_INSTALLER"), "/usr/share/routeglass/agent.sh", "install/agent.sh"}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if b, e := os.ReadFile(p); e == nil {
			w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write([]byte(strings.ReplaceAll(string(b), "__ROUTEGLASS_SERVER_ORIGIN__", s.cfg.PublicOrigin)))
			return
		}
	}
	fail(w, r, 404, "installer_unavailable")
}
func (s *Server) network(w http.ResponseWriter, r *http.Request) {
	rows, e := s.store.DB.QueryContext(r.Context(), `SELECT id,name,endpoint,status,enabled,tls_ready,observed_ip,asn,network,region,city,latitude,longitude,ip_family,last_seen FROM nodes ORDER BY name`)
	if e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, n, ep, st, ip, asn, nw, region, city, family string
		var enabled, tls int
		var lat, lon *float64
		var seen *int64
		rows.Scan(&id, &n, &ep, &st, &enabled, &tls, &ip, &asn, &nw, &region, &city, &lat, &lon, &family, &seen)
		out = append(out, map[string]any{"id": id, "name": n, "endpoint": ep, "status": st, "enabled": enabled == 1, "tls_ready": tls == 1, "observed_ip": ip, "asn": asn, "network": nw, "region": region, "city": city, "latitude": lat, "longitude": lon, "ip_family": family, "last_seen": seen})
	}
	jsonOut(w, 200, map[string]any{"observed_ip": netutil.ClientIP(r, s.trusted).String(), "asn": "unknown", "network": "unknown", "region": "unknown", "ip_family": ipFamily(netutil.ClientIP(r, s.trusted)), "nodes": out})
}
func ipFamily(a netip.Addr) string {
	if a.Is4() {
		return "ipv4"
	}
	if a.Is6() {
		return "ipv6"
	}
	return "unknown"
}

type access struct {
	ID, IP, CSRF string
	CSRFHash     []byte
	Expires      int64
}

func (s *Server) accessCookie() string {
	if s.cfg.InsecureCookies {
		return "rg_access"
	}
	return "__Host-rg_access"
}
func (s *Server) adminCookie() string {
	if s.cfg.InsecureCookies {
		return "rg_admin"
	}
	return "__Host-rg_admin"
}
func (s *Server) getAccess(r *http.Request) *access {
	c, e := r.Cookie(s.accessCookie())
	if e != nil {
		return nil
	}
	h := auth.HashToken(c.Value)
	var a access
	e = s.store.DB.QueryRowContext(r.Context(), `SELECT id,observed_ip,csrf_token,csrf_hash,expires_at FROM access_sessions WHERE token_hash=? AND expires_at>=?`, h, time.Now().Unix()).Scan(&a.ID, &a.IP, &a.CSRF, &a.CSRFHash, &a.Expires)
	if e != nil {
		return nil
	}
	return &a
}
func (s *Server) newAccess(w http.ResponseWriter, r *http.Request) (*access, error) {
	tok, e := auth.RandomToken(32)
	if e != nil {
		return nil, e
	}
	csrf, e := auth.RandomToken(24)
	if e != nil {
		return nil, e
	}
	id, _ := auth.RandomToken(16)
	ip := netutil.ClientIP(r, s.trusted).String()
	exp := time.Now().Add(30 * time.Minute).Unix()
	_, e = s.store.DB.ExecContext(r.Context(), `INSERT INTO access_sessions(id,token_hash,csrf_hash,csrf_token,observed_ip,expires_at,created_at) VALUES(?,?,?,?,?,?,?)`, id, auth.HashToken(tok), auth.HashToken(csrf), csrf, ip, exp, time.Now().Unix())
	if e != nil {
		return nil, e
	}
	http.SetCookie(w, &http.Cookie{Name: s.accessCookie(), Value: tok, Path: "/", HttpOnly: true, Secure: !s.cfg.InsecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 1800})
	return &access{ID: id, IP: ip, CSRF: csrf, CSRFHash: auth.HashToken(csrf), Expires: exp}, nil
}
func (s *Server) validateOrigin(r *http.Request) bool {
	return s.cfg.PublicOrigin == "" || r.Header.Get("Origin") == s.cfg.PublicOrigin
}
func (s *Server) validateAccessWrite(r *http.Request, a *access) bool {
	return a != nil && s.validateOrigin(r) && subtle.ConstantTimeCompare(auth.HashToken(r.Header.Get("X-CSRF-Token")), a.CSRFHash) == 1
}
func (s *Server) accessCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	limitKey := "access:" + netutil.ClientIP(r, s.trusted).String()
	if !s.allowAttempt(limitKey, 10, 10*time.Minute) {
		fail(w, r, 429, "access_rate_limited")
		return
	}
	g, _ := strconv.ParseUint(s.store.Setting(r.Context(), "access_generation", "0"), 10, 64)
	secret, _ := base64.RawURLEncoding.DecodeString(s.sec.AccessSecret)
	if !auth.VerifyAccessCode(secret, g, in.Code, time.Now(), 10*time.Minute, 30*time.Second) {
		fail(w, r, 401, "invalid_access_code")
		return
	}
	s.clearAttempts(limitKey)
	a, e := s.newAccess(w, r)
	if e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	jsonOut(w, 201, a)
}
func (s *Server) accessInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	if e := s.store.ConsumeOneTime(r.Context(), "invites", auth.HashToken(in.Token), time.Now()); e != nil {
		fail(w, r, 401, "invalid_invite")
		return
	}
	a, e := s.newAccess(w, r)
	if e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	jsonOut(w, 201, a)
}
func (s *Server) accessSession(w http.ResponseWriter, r *http.Request) {
	a := s.getAccess(r)
	if a == nil {
		fail(w, r, 401, "access_required")
		return
	}
	jsonOut(w, 200, a)
}
func (s *Server) accessLogout(w http.ResponseWriter, r *http.Request) {
	a := s.getAccess(r)
	if !s.validateAccessWrite(r, a) {
		fail(w, r, 403, "invalid_csrf")
		return
	}
	c, e := r.Cookie(s.accessCookie())
	if e == nil {
		s.store.DB.ExecContext(r.Context(), `DELETE FROM access_sessions WHERE token_hash=?`, auth.HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: s.accessCookie(), Value: "", Path: "/", MaxAge: -1, Secure: !s.cfg.InsecureCookies})
	w.WriteHeader(204)
}

func (s *Server) createTest(w http.ResponseWriter, r *http.Request) {
	a := s.getAccess(r)
	if a == nil {
		fail(w, r, 401, "access_required")
		return
	}
	if !s.validateAccessWrite(r, a) {
		fail(w, r, 403, "invalid_csrf")
		return
	}
	var n int
	s.store.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM tests WHERE session_id=? AND created_at>?`, a.ID, time.Now().Add(-10*time.Minute).Unix()).Scan(&n)
	if n >= 5 {
		fail(w, r, 429, "test_rate_limited")
		return
	}
	var last int64
	_ = s.store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(created_at),0) FROM tests WHERE session_id=?`, a.ID).Scan(&last)
	if last > time.Now().Add(-30*time.Second).Unix() {
		fail(w, r, 429, "test_cooldown")
		return
	}
	id, _ := auth.RandomToken(16)
	_, e := s.store.DB.ExecContext(r.Context(), `INSERT INTO tests(id,session_id,status,client_ip,created_at) VALUES(?,?,'nodes',?,?)`, id, a.ID, a.IP, time.Now().Unix())
	if e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	jsonOut(w, 201, map[string]any{"id": id, "status": "nodes"})
}
func (s *Server) testOwned(r *http.Request, id string) (*access, bool) {
	a := s.getAccess(r)
	if a == nil {
		return nil, false
	}
	if r.Method != "GET" && r.Method != "HEAD" && !s.validateAccessWrite(r, a) {
		return a, false
	}
	var n int
	s.store.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM tests WHERE id=? AND session_id=?`, id, a.ID).Scan(&n)
	return a, n == 1
}
func (s *Server) getTest(w http.ResponseWriter, r *http.Request) {
	_, ok := s.testOwned(r, r.PathValue("id"))
	if !ok {
		fail(w, r, 404, "test_not_found")
		return
	}
	var status string
	var created int64
	s.store.DB.QueryRowContext(r.Context(), `SELECT status,created_at FROM tests WHERE id=?`, r.PathValue("id")).Scan(&status, &created)
	rows, _ := s.store.DB.QueryContext(r.Context(), `SELECT node_id,latency_ms,loss_percent,jitter_ms,download_mbps,upload_mbps,score,status FROM test_node_results WHERE test_id=?`, r.PathValue("id"))
	defer rows.Close()
	results := []map[string]any{}
	for rows.Next() {
		var node, st string
		var l, loss, j, d, u, sc *float64
		rows.Scan(&node, &l, &loss, &j, &d, &u, &sc, &st)
		results = append(results, map[string]any{"node_id": node, "latency_ms": l, "loss_percent": loss, "jitter_ms": j, "download_mbps": d, "upload_mbps": u, "score": sc, "status": st})
	}
	hr, _ := s.store.DB.QueryContext(r.Context(), `SELECT node_id,hop,ip_anon,asn,region,latency_ms,geo_quality FROM route_hops WHERE test_id=? ORDER BY node_id,hop`, r.PathValue("id"))
	defer hr.Close()
	hops := []map[string]any{}
	for hr.Next() {
		var node, quality string
		var hop int
		var ip, asn, region *string
		var ms *float64
		hr.Scan(&node, &hop, &ip, &asn, &region, &ms, &quality)
		hops = append(hops, map[string]any{"node_id": node, "hop": hop, "ip": ip, "asn": asn, "region": region, "latency_ms": ms, "geo_quality": quality})
	}
	jsonOut(w, 200, map[string]any{"id": r.PathValue("id"), "status": status, "created_at": created, "results": results, "route_hops": hops})
}
func (s *Server) probes(w http.ResponseWriter, r *http.Request) {
	_, ok := s.testOwned(r, r.PathValue("id"))
	if !ok {
		fail(w, r, 404, "test_not_found")
		return
	}
	var in struct {
		NodeID  string  `json:"node_id"`
		Latency float64 `json:"latency_ms"`
		Loss    float64 `json:"loss_percent"`
		Jitter  float64 `json:"jitter_ms"`
	}
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	_, e := s.store.DB.ExecContext(r.Context(), `INSERT INTO test_node_results(test_id,node_id,latency_ms,loss_percent,jitter_ms,status) VALUES(?,?,?,?,?,'probed') ON CONFLICT(test_id,node_id) DO UPDATE SET latency_ms=excluded.latency_ms,loss_percent=excluded.loss_percent,jitter_ms=excluded.jitter_ms,status='probed'`, r.PathValue("id"), in.NodeID, in.Latency, in.Loss, in.Jitter)
	if e != nil {
		fail(w, r, 400, "invalid_node")
		return
	}
	w.WriteHeader(204)
}
func (s *Server) grants(w http.ResponseWriter, r *http.Request) {
	a, ok := s.testOwned(r, r.PathValue("id"))
	if !ok {
		fail(w, r, 404, "test_not_found")
		return
	}
	var in struct {
		NodeID string   `json:"node_id"`
		Scopes []string `json:"scopes"`
	}
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	var n int
	s.store.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM nodes WHERE id=? AND enabled=1 AND status='online' AND tls_ready=1`, in.NodeID).Scan(&n)
	if n != 1 {
		fail(w, r, 409, "node_unavailable")
		return
	}
	now := time.Now()
	c := grant.Claims{KeyID: s.sec.KeyID, NodeID: in.NodeID, SessionID: a.ID, TestID: r.PathValue("id"), ClientIP: a.IP, Scopes: in.Scopes, MaxDownloadBytes: 150_000_000, MaxUploadBytes: 100_000_000, MaxStreams: 4, NotBefore: now.Add(-5 * time.Second).Unix(), Expires: now.Add(60 * time.Second).Unix()}
	t, e := grant.Sign(s.priv, c)
	if e != nil {
		fail(w, r, 500, "grant_error")
		return
	}
	jsonOut(w, 201, map[string]string{"grant": t})
}
func (s *Server) results(w http.ResponseWriter, r *http.Request) {
	_, ok := s.testOwned(r, r.PathValue("id"))
	if !ok {
		fail(w, r, 404, "test_not_found")
		return
	}
	var in struct {
		NodeID   string  `json:"node_id"`
		Latency  float64 `json:"latency_ms"`
		Loss     float64 `json:"loss_percent"`
		Jitter   float64 `json:"jitter_ms"`
		Download float64 `json:"download_mbps"`
		Upload   float64 `json:"upload_mbps"`
	}
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	b := score.Calculate(score.Metrics{LatencyMS: in.Latency, LossPercent: in.Loss, JitterMS: in.Jitter, DownloadMbps: in.Download, UploadMbps: in.Upload})
	_, e := s.store.DB.ExecContext(r.Context(), `INSERT INTO test_node_results(test_id,node_id,latency_ms,loss_percent,jitter_ms,download_mbps,upload_mbps,score,status) VALUES(?,?,?,?,?,?,?,?,'complete') ON CONFLICT(test_id,node_id) DO UPDATE SET latency_ms=?,loss_percent=?,jitter_ms=?,download_mbps=?,upload_mbps=?,score=?,status='complete'`, r.PathValue("id"), in.NodeID, in.Latency, in.Loss, in.Jitter, in.Download, in.Upload, b.Overall, in.Latency, in.Loss, in.Jitter, in.Download, in.Upload, b.Overall)
	if e != nil {
		fail(w, r, 400, "invalid_result")
		return
	}
	s.store.DB.ExecContext(r.Context(), `UPDATE tests SET status='complete',completed_at=? WHERE id=?`, time.Now().Unix(), r.PathValue("id"))
	jsonOut(w, 200, b)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	_, ok := s.testOwned(r, r.PathValue("id"))
	if !ok {
		fail(w, r, 404, "test_not_found")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, "event: snapshot\ndata: {\"test_id\":%q}\n\n", r.PathValue("id"))
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	a, ok := s.testOwned(r, r.PathValue("id"))
	if !ok {
		fail(w, r, 404, "test_not_found")
		return
	}
	var in struct {
		NodeID string `json:"node_id"`
	}
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	v, ok := s.controls.Load(in.NodeID)
	if !ok {
		fail(w, r, 409, "node_offline")
		return
	}
	c := v.(*controlConn)
	cmd := controlMessage{Type: "route", ID: r.PathValue("id"), Target: a.IP}
	select {
	case c.send <- cmd:
		jsonOut(w, 202, map[string]string{"status": "queued"})
	default:
		fail(w, r, 503, "node_busy")
	}
}

type adminSession struct {
	ID         string
	CSRFHash   []byte
	CSRF       string
	MustChange bool
}

func (s *Server) getAdmin(r *http.Request) *adminSession {
	c, e := r.Cookie(s.adminCookie())
	if e != nil {
		return nil
	}
	var id, csrfToken string
	var csrf []byte
	var must int
	e = s.store.DB.QueryRowContext(r.Context(), `SELECT a.id,s.csrf_hash,s.csrf_token,a.must_change FROM admin_sessions s JOIN admins a ON a.id=s.admin_id WHERE s.id_hash=? AND s.expires_at>=?`, auth.HashToken(c.Value), time.Now().Unix()).Scan(&id, &csrf, &csrfToken, &must)
	if e != nil {
		return nil
	}
	return &adminSession{id, csrf, csrfToken, must == 1}
}
func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	limitKey := "admin:" + netutil.ClientIP(r, s.trusted).String() + ":" + strings.ToLower(in.Username)
	if !s.allowAttempt(limitKey, 5, 10*time.Minute) {
		fail(w, r, 429, "admin_login_rate_limited")
		return
	}
	var id, h string
	var must int
	if s.store.DB.QueryRowContext(r.Context(), `SELECT id,password_hash,must_change FROM admins WHERE username=?`, in.Username).Scan(&id, &h, &must) != nil || !auth.VerifyPassword(h, in.Password) {
		fail(w, r, 401, "invalid_credentials")
		return
	}
	s.clearAttempts(limitKey)
	tok, _ := auth.RandomToken(32)
	csrf, _ := auth.RandomToken(24)
	_, e := s.store.DB.ExecContext(r.Context(), `INSERT INTO admin_sessions(id_hash,admin_id,csrf_hash,csrf_token,expires_at,created_at) VALUES(?,?,?,?,?,?)`, auth.HashToken(tok), id, auth.HashToken(csrf), csrf, time.Now().Add(12*time.Hour).Unix(), time.Now().Unix())
	if e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.adminCookie(), Value: tok, Path: "/", HttpOnly: true, Secure: !s.cfg.InsecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	jsonOut(w, 200, map[string]any{"csrf": csrf, "must_change_password": must == 1})
}
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) *adminSession {
	a := s.getAdmin(r)
	if a == nil {
		fail(w, r, 401, "admin_required")
		return nil
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		if !s.validateOrigin(r) {
			fail(w, r, 403, "invalid_origin")
			return nil
		}
		got := auth.HashToken(r.Header.Get("X-CSRF-Token"))
		if subtle.ConstantTimeCompare(got, a.CSRFHash) != 1 {
			fail(w, r, 403, "invalid_csrf")
			return nil
		}
	}
	return a
}
func (s *Server) adminPassword(w http.ResponseWriter, r *http.Request) {
	a := s.requireAdmin(w, r)
	if a == nil {
		return
	}
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if decode(r, &in) != nil || len(in.New) < 12 {
		fail(w, r, 400, "invalid_password")
		return
	}
	var h string
	if s.store.DB.QueryRowContext(r.Context(), `SELECT password_hash FROM admins WHERE id=?`, a.ID).Scan(&h) != nil || !auth.VerifyPassword(h, in.Current) {
		fail(w, r, 401, "invalid_credentials")
		return
	}
	next, e := auth.HashPassword(in.New)
	if e != nil {
		fail(w, r, 500, "password_error")
		return
	}
	if _, e = s.store.DB.ExecContext(r.Context(), `UPDATE admins SET password_hash=?,must_change=0 WHERE id=?`, next, a.ID); e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	w.WriteHeader(204)
}
func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	a := s.requireAdmin(w, r)
	if a == nil {
		return
	}
	c, _ := r.Cookie(s.adminCookie())
	s.store.DB.ExecContext(r.Context(), `DELETE FROM admin_sessions WHERE id_hash=?`, auth.HashToken(c.Value))
	http.SetCookie(w, &http.Cookie{Name: s.adminCookie(), Value: "", Path: "/", MaxAge: -1, Secure: !s.cfg.InsecureCookies})
	w.WriteHeader(204)
}
func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	a := s.requireAdmin(w, r)
	if a == nil {
		return
	}
	var nodes, sessions, tests int
	s.store.DB.QueryRow(`SELECT count(*) FROM nodes WHERE status='online'`).Scan(&nodes)
	s.store.DB.QueryRow(`SELECT count(*) FROM access_sessions WHERE expires_at>=?`, time.Now().Unix()).Scan(&sessions)
	s.store.DB.QueryRow(`SELECT count(*) FROM tests WHERE created_at>=?`, time.Now().Add(-24*time.Hour).Unix()).Scan(&tests)
	jsonOut(w, 200, map[string]any{"online_nodes": nodes, "active_sessions": sessions, "tests_24h": tests, "csrf": a.CSRF})
}
func (s *Server) adminAccess(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	g, _ := strconv.ParseUint(s.store.Setting(r.Context(), "access_generation", "0"), 10, 64)
	secret, _ := base64.RawURLEncoding.DecodeString(s.sec.AccessSecret)
	jsonOut(w, 200, map[string]any{"code": auth.AccessCode(secret, g, time.Now(), 10*time.Minute), "generation": g, "rotates_seconds": 600})
}
func (s *Server) rotateAccess(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	g, _ := strconv.ParseUint(s.store.Setting(r.Context(), "access_generation", "0"), 10, 64)
	s.store.PutSetting(r.Context(), "access_generation", strconv.FormatUint(g+1, 10))
	s.adminAccess(w, r)
}
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	tok, _ := auth.RandomToken(32)
	id, _ := auth.RandomToken(12)
	exp := time.Now().Add(30 * time.Minute).Unix()
	s.store.DB.ExecContext(r.Context(), `INSERT INTO invites(id,token_hash,expires_at,created_at) VALUES(?,?,?,?)`, id, auth.HashToken(tok), exp, time.Now().Unix())
	jsonOut(w, 201, map[string]any{"id": id, "token": tok, "expires_at": exp})
}
func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	rows, _ := s.store.DB.QueryContext(r.Context(), `SELECT id,expires_at,used_at,created_at FROM invites ORDER BY created_at DESC`)
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id string
		var exp, created int64
		var used *int64
		rows.Scan(&id, &exp, &used, &created)
		out = append(out, map[string]any{"id": id, "expires_at": exp, "used_at": used, "created_at": created})
	}
	jsonOut(w, 200, map[string]any{"invites": out})
}
func (s *Server) createJoin(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	var in struct {
		Name, Region, City  string
		Latitude, Longitude *float64
	}
	if r.ContentLength > 0 && decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	tok, _ := auth.RandomToken(32)
	tok = "rgj_" + tok
	id, _ := auth.RandomToken(12)
	nodeID, _ := auth.RandomToken(12)
	if in.Name == "" {
		in.Name = "New node"
	}
	exp := time.Now().Add(10 * time.Minute).Unix()
	tx, e := s.store.DB.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	defer tx.Rollback()
	_, e = tx.Exec(`INSERT INTO nodes(id,name,endpoint,region,city,latitude,longitude,created_at) VALUES(?,?,'',?,?,?,?,?)`, nodeID, in.Name, unknown(in.Region), unknown(in.City), in.Latitude, in.Longitude, time.Now().Unix())
	if e == nil {
		_, e = tx.Exec(`INSERT INTO join_tokens(id,token_hash,node_id,expires_at,created_at) VALUES(?,?,?,?,?)`, id, auth.HashToken(tok), nodeID, exp, time.Now().Unix())
	}
	if e != nil || tx.Commit() != nil {
		fail(w, r, 500, "database_error")
		return
	}
	origin := strings.TrimRight(s.cfg.PublicOrigin, "/")
	jsonOut(w, 201, map[string]any{"id": id, "node_id": nodeID, "token": tok, "expires_at": exp, "install_command": fmt.Sprintf("curl -fsSL %s/install/agent.sh | sudo sh -s -- --join %s", origin, tok)})
}
func unknown(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}
func (s *Server) adminNodes(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	s.network(w, r)
}
func (s *Server) updateNode(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	var in struct {
		Name      string   `json:"name"`
		Endpoint  string   `json:"endpoint"`
		Region    string   `json:"region"`
		City      string   `json:"city"`
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
		Enabled   *bool    `json:"enabled"`
	}
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	var current struct {
		Name, Endpoint, Region, City string
		Lat, Lon                     *float64
		Enabled                      int
	}
	if s.store.DB.QueryRowContext(r.Context(), `SELECT name,endpoint,region,city,latitude,longitude,enabled FROM nodes WHERE id=?`, r.PathValue("id")).Scan(&current.Name, &current.Endpoint, &current.Region, &current.City, &current.Lat, &current.Lon, &current.Enabled) != nil {
		fail(w, r, 404, "node_not_found")
		return
	}
	if in.Name != "" {
		current.Name = in.Name
	}
	if in.Endpoint != "" {
		current.Endpoint = in.Endpoint
	}
	if in.Region != "" {
		current.Region = in.Region
	}
	if in.City != "" {
		current.City = in.City
	}
	if in.Latitude != nil {
		current.Lat = in.Latitude
	}
	if in.Longitude != nil {
		current.Lon = in.Longitude
	}
	if in.Enabled != nil {
		current.Enabled = boolInt(*in.Enabled)
	}
	_, e := s.store.DB.ExecContext(r.Context(), `UPDATE nodes SET name=?,endpoint=?,region=?,city=?,latitude=?,longitude=?,enabled=? WHERE id=?`, current.Name, current.Endpoint, current.Region, current.City, current.Lat, current.Lon, current.Enabled, r.PathValue("id"))
	if e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	w.WriteHeader(204)
}
func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	res, e := s.store.DB.ExecContext(r.Context(), `DELETE FROM nodes WHERE id=? AND status='offline'`, r.PathValue("id"))
	if e != nil {
		fail(w, r, 409, "node_in_use")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, r, 409, "node_online_or_missing")
		return
	}
	w.WriteHeader(204)
}
func (s *Server) adminSessions(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	rows, _ := s.store.DB.QueryContext(r.Context(), `SELECT id,observed_ip,expires_at,created_at FROM access_sessions WHERE expires_at>=?`, time.Now().Unix())
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, ip string
		var exp, created int64
		rows.Scan(&id, &ip, &exp, &created)
		out = append(out, map[string]any{"id": id, "ip": ip, "expires_at": exp, "created_at": created})
	}
	jsonOut(w, 200, map[string]any{"sessions": out})
}
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	res, e := s.store.DB.ExecContext(r.Context(), `DELETE FROM access_sessions WHERE id=?`, r.PathValue("id"))
	if e != nil {
		fail(w, r, 500, "database_error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, r, 404, "session_not_found")
		return
	}
	w.WriteHeader(204)
}
func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	if r.Method == "GET" {
		jsonOut(w, 200, map[string]string{"history_days": s.store.Setting(r.Context(), "history_days", "30")})
		return
	}
	var in map[string]string
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	for k, v := range in {
		if k == "history_days" {
			s.store.PutSetting(r.Context(), k, v)
		}
	}
	w.WriteHeader(204)
}

type joinRequest struct {
	Token     string `json:"token"`
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	PublicKey string `json:"public_key"`
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var in joinRequest
	if decode(r, &in) != nil {
		fail(w, r, 400, "invalid_json")
		return
	}
	hash := auth.HashToken(in.Token)
	var assigned string
	if e := s.store.DB.QueryRowContext(r.Context(), `SELECT node_id FROM join_tokens WHERE token_hash=? AND used_at IS NULL AND expires_at>=?`, hash, time.Now().Unix()).Scan(&assigned); e != nil {
		fail(w, r, 401, "invalid_join_token")
		return
	}
	if e := s.store.ConsumeOneTime(r.Context(), "join_tokens", hash, time.Now()); e != nil {
		fail(w, r, 401, "invalid_join_token")
		return
	}
	pk, e := base64.RawStdEncoding.DecodeString(in.PublicKey)
	if e != nil || len(pk) != ed25519.PublicKeySize {
		fail(w, r, 400, "invalid_public_key")
		return
	}
	if assigned != "" {
		in.NodeID = assigned
	}
	if in.NodeID == "" {
		in.NodeID, _ = auth.RandomToken(12)
	}
	if in.Name == "" {
		in.Name = in.NodeID
	}
	ip := netutil.ClientIP(r, s.trusted)
	res, e := s.store.DB.ExecContext(r.Context(), `UPDATE nodes SET name=CASE WHEN name='New node' THEN ? ELSE name END,endpoint=?,public_key=?,observed_ip=?,ip_family=? WHERE id=?`, in.Name, in.Endpoint, pk, ip.String(), ipFamily(ip), in.NodeID)
	if e == nil {
		n, _ := res.RowsAffected()
		if n == 0 {
			_, e = s.store.DB.ExecContext(r.Context(), `INSERT INTO nodes(id,name,endpoint,public_key,observed_ip,ip_family,status,tls_ready,last_seen,created_at) VALUES(?,?,?,?,?,?,'offline',0,NULL,?)`, in.NodeID, in.Name, in.Endpoint, pk, ip.String(), ipFamily(ip), time.Now().Unix())
		}
	}
	if e != nil {
		fail(w, r, 409, "node_exists")
		return
	}
	jsonOut(w, 201, map[string]string{"node_id": in.NodeID, "server_public_key": s.sec.PublicKey})
}

type controlMessage struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Target   string `json:"target,omitempty"`
	At       int64  `json:"at,omitempty"`
	TLSReady bool   `json:"tls_ready,omitempty"`
	Hops     any    `json:"hops,omitempty"`
	Error    string `json:"error,omitempty"`
}
type controlConn struct{ send chan controlMessage }

func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	node := r.Header.Get("X-RouteGlass-Node")
	ts := r.Header.Get("X-RouteGlass-Timestamp")
	nonce := r.Header.Get("X-RouteGlass-Nonce")
	sig64 := r.Header.Get("X-RouteGlass-Signature")
	var pk []byte
	if s.store.DB.QueryRowContext(r.Context(), `SELECT public_key FROM nodes WHERE id=?`, node).Scan(&pk) != nil {
		fail(w, r, 401, "unknown_node")
		return
	}
	t, e := strconv.ParseInt(ts, 10, 64)
	sig, e2 := base64.RawURLEncoding.DecodeString(sig64)
	msg := strings.Join([]string{ts, nonce, "GET", "/api/v1/agent/control"}, "\n")
	if e != nil || e2 != nil || time.Since(time.Unix(t, 0)) > 2*time.Minute || time.Until(time.Unix(t, 0)) > 2*time.Minute || !ed25519.Verify(pk, []byte(msg), sig) {
		fail(w, r, 401, "invalid_node_signature")
		return
	}
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if e != nil {
		return
	}
	defer c.CloseNow()
	cc := &controlConn{send: make(chan controlMessage, 8)}
	s.controls.Store(node, cc)
	defer s.controls.Delete(node)
	s.store.DB.ExecContext(r.Context(), `UPDATE nodes SET status='online',tls_ready=0,last_seen=? WHERE id=?`, time.Now().Unix(), node)
	defer s.store.DB.Exec(`UPDATE nodes SET status='offline',tls_ready=0 WHERE id=?`, node)
	ctx := r.Context()
	go func() {
		for {
			var m controlMessage
			if wsjson.Read(ctx, c, &m) != nil {
				return
			}
			switch m.Type {
			case "heartbeat":
				s.store.DB.Exec(`UPDATE nodes SET last_seen=?,status='online',tls_ready=? WHERE id=?`, time.Now().Unix(), boolInt(m.TLSReady), node)
			case "route_result":
				if m.Error == "" {
					s.store.DB.Exec(`DELETE FROM route_hops WHERE test_id=? AND node_id=?`, m.ID, node)
					if hops, ok := m.Hops.([]any); ok {
						for i, h := range hops {
							num := i + 1
							addr := ""
							var ms any = nil
							if hm, ok := h.(map[string]any); ok {
								if v, ok := hm["hop"].(float64); ok {
									num = int(v)
								}
								addr, _ = hm["address"].(string)
								ms = hm["latency_ms"]
							}
							s.store.DB.Exec(`INSERT INTO route_hops(test_id,node_id,hop,ip_anon,latency_ms,geo_quality) VALUES(?,?,?,?,?,'unknown')`, m.ID, node, num, anonymizeIP(addr), ms)
						}
					}
					s.store.DB.Exec(`UPDATE tests SET status='route_complete' WHERE id=?`, m.ID)
				} else {
					s.store.DB.Exec(`UPDATE tests SET status='route_failed' WHERE id=?`, m.ID)
				}
			}
		}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case m := <-cc.send:
			if wsjson.Write(ctx, c, m) != nil {
				return
			}
		case <-ticker.C:
			if wsjson.Write(ctx, c, controlMessage{Type: "ping", At: time.Now().Unix()}) != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func anonymizeIP(raw string) string {
	a, e := netip.ParseAddr(raw)
	if e != nil {
		return ""
	}
	if a.Is4() {
		b := a.As4()
		b[3] = 0
		return netip.AddrFrom4(b).String() + "/24"
	}
	b := a.As16()
	for i := 6; i < len(b); i++ {
		b[i] = 0
	}
	return netip.AddrFrom16(b).String() + "/48"
}
