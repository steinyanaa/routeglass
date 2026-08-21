package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/steinyanaa/routeglass/internal/auth"
	"github.com/steinyanaa/routeglass/internal/grant"
	"github.com/steinyanaa/routeglass/internal/routeexec"
	"github.com/steinyanaa/routeglass/internal/tlsprovider"
)

type Config struct {
	Listen             string   `json:"listen"`
	DataDir            string   `json:"data_dir"`
	ServerURL          string   `json:"server_url"`
	JoinToken          string   `json:"join_token,omitempty"`
	NodeID             string   `json:"node_id,omitempty"`
	Name               string   `json:"name"`
	Endpoint           string   `json:"endpoint"`
	AllowedOrigin      string   `json:"allowed_origin"`
	TLSProvider        string   `json:"tls_provider,omitempty"`
	TLSCert            string   `json:"tls_cert"`
	TLSKey             string   `json:"tls_key"`
	ACMEEmail          string   `json:"acme_email,omitempty"`
	ACMENames          []string `json:"acme_names,omitempty"`
	ACMEHTTPAddress    string   `json:"acme_http_address,omitempty"`
	ACMECAURL          string   `json:"acme_ca_url,omitempty"`
	InsecureData       bool     `json:"insecure_data,omitempty"`
	IdentityPrivateKey string   `json:"identity_private_key,omitempty"`
	ServerPublicKey    string   `json:"server_public_key,omitempty"`
	DailyQuotaBytes    int64    `json:"daily_quota_bytes,omitempty"`
	MaxConcurrentTests int      `json:"max_concurrent_tests,omitempty"`
}
type Agent struct {
	cfg       Config
	cfgPath   string
	priv      ed25519.PrivateKey
	serverPub ed25519.PublicKey
	mux       *http.ServeMux
	ledger    *ledger
	tlsReady  atomic.Bool
	cert      atomic.Pointer[tls.Certificate]
}
type usage struct {
	Down, Up int64
	Streams  int
	Expires  int64
}
type ledger struct {
	mu        sync.Mutex
	byNonce   map[string]*usage
	active    map[string]int
	day       string
	daily     int64
	limit     int64
	maxActive int
	path      string
}

type quotaState struct {
	Day   string `json:"day"`
	Bytes int64  `json:"bytes"`
}

func LoadConfig(path string) (Config, error) {
	c := Config{Listen: ":9443", DataDir: "./agent-data", DailyQuotaBytes: 50 * 1024 * 1024 * 1024, MaxConcurrentTests: 3}
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func New(cfg Config, path string) (*Agent, error) {
	if cfg.Listen == "" {
		cfg.Listen = ":9443"
	}
	if cfg.DailyQuotaBytes == 0 {
		cfg.DailyQuotaBytes = 50 * 1024 * 1024 * 1024
	}
	if cfg.MaxConcurrentTests <= 0 {
		cfg.MaxConcurrentTests = 3
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, err
	}
	a := &Agent{cfg: cfg, cfgPath: path, mux: http.NewServeMux(), ledger: &ledger{byNonce: map[string]*usage{}, active: map[string]int{}, limit: cfg.DailyQuotaBytes, maxActive: cfg.MaxConcurrentTests, path: filepath.Join(cfg.DataDir, "quota.json")}}
	a.ledger.load()
	if cfg.IdentityPrivateKey != "" {
		b, e := base64.RawStdEncoding.DecodeString(cfg.IdentityPrivateKey)
		if e != nil || len(b) != ed25519.PrivateKeySize {
			return nil, errors.New("invalid identity_private_key")
		}
		a.priv = ed25519.PrivateKey(b)
	} else {
		_, p, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		a.priv = p
	}
	if cfg.ServerPublicKey != "" {
		b, e := base64.RawStdEncoding.DecodeString(cfg.ServerPublicKey)
		if e != nil || len(b) != ed25519.PublicKeySize {
			return nil, errors.New("invalid server_public_key")
		}
		a.serverPub = ed25519.PublicKey(b)
	}
	a.routes()
	return a, nil
}
func (a *Agent) routes() {
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	a.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if len(a.serverPub) == 0 {
			http.Error(w, "not joined", 503)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	a.mux.HandleFunc("GET /agent/v1/probe", a.probe)
	a.mux.HandleFunc("GET /agent/v1/download", a.download)
	a.mux.HandleFunc("POST /agent/v1/upload", a.upload)
	a.mux.HandleFunc("OPTIONS /agent/v1/probe", a.preflight)
	a.mux.HandleFunc("OPTIONS /agent/v1/download", a.preflight)
	a.mux.HandleFunc("OPTIONS /agent/v1/upload", a.preflight)
}
func (a *Agent) Run(ctx context.Context) error {
	if a.cfg.JoinToken != "" {
		if e := a.join(ctx); e != nil {
			return fmt.Errorf("join: %w", e)
		}
	}
	if len(a.serverPub) == 0 {
		return errors.New("agent is not joined")
	}
	go a.controlLoop(ctx)
	var provider tlsprovider.Provider
	if a.cfg.InsecureData {
		a.tlsReady.Store(true)
	} else if a.cfg.TLSCert != "" && a.cfg.TLSKey != "" {
		provider = &tlsprovider.Files{CertFile: a.cfg.TLSCert, KeyFile: a.cfg.TLSKey}
		if strings.HasPrefix(a.cfg.TLSProvider, "acme-") {
			ap, e := tlsprovider.NewACME(tlsprovider.ACMEConfig{Mode: a.cfg.TLSProvider, Email: a.cfg.ACMEEmail, Names: a.cfg.ACMENames, CertFile: a.cfg.TLSCert, KeyFile: a.cfg.TLSKey, AccountKeyFile: filepath.Join(a.cfg.DataDir, "acme-account.pem"), HTTPAddress: a.cfg.ACMEHTTPAddress, CADirURL: a.cfg.ACMECAURL})
			if e != nil {
				return e
			}
			provider = ap
			if _, _, e = provider.Load(ctx); e != nil {
				if e = provider.Renew(ctx); e != nil {
					slog.Error("TLS certificate unavailable; data endpoints disabled", "error", e)
				}
			}
		}
		if cert, _, e := provider.Load(ctx); e == nil {
			a.cert.Store(cert)
			a.tlsReady.Store(true)
		}
	}
	if provider != nil {
		go a.renewLoop(ctx, provider)
	}
	h := &http.Server{Addr: a.cfg.Listen, Handler: a.mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		h.Shutdown(c)
	}()
	var e error
	if a.cfg.InsecureData {
		e = h.ListenAndServe()
	} else {
		ln, le := net.Listen("tcp", a.cfg.Listen)
		if le != nil {
			return le
		}
		tlsLn := tls.NewListener(ln, &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			c := a.cert.Load()
			if c == nil {
				return nil, errors.New("TLS certificate unavailable")
			}
			return c, nil
		}})
		e = h.Serve(tlsLn)
	}
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func (a *Agent) renewLoop(ctx context.Context, p tlsprovider.Provider) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cert, st, e := p.Load(ctx)
			if e == nil && time.Now().Before(st.RenewAt) {
				a.cert.Store(cert)
				a.tlsReady.Store(true)
				continue
			}
			if strings.HasPrefix(a.cfg.TLSProvider, "acme-") {
				if e = p.Renew(ctx); e != nil {
					slog.Error("TLS renewal failed; retaining current certificate", "error", e)
					if !st.NotAfter.IsZero() && st.NotAfter.Before(time.Now()) {
						a.tlsReady.Store(false)
					}
					continue
				}
			}
			cert, _, e = p.Load(ctx)
			if e != nil {
				a.tlsReady.Store(false)
				continue
			}
			a.cert.Store(cert)
			a.tlsReady.Store(true)
		case <-ctx.Done():
			return
		}
	}
}
func (a *Agent) join(ctx context.Context) error {
	body := map[string]string{
		"token":      a.cfg.JoinToken,
		"node_id":    a.cfg.NodeID,
		"name":       a.cfg.Name,
		"endpoint":   a.cfg.Endpoint,
		"public_key": base64.RawStdEncoding.EncodeToString(a.priv.Public().(ed25519.PublicKey)),
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(a.cfg.ServerURL, "/")+"/api/v1/agent/join", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		x, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server returned %s: %s", resp.Status, string(x))
	}
	var out struct {
		NodeID          string `json:"node_id"`
		ServerPublicKey string `json:"server_public_key"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&out); e != nil {
		return e
	}
	a.cfg.NodeID = out.NodeID
	a.cfg.ServerPublicKey = out.ServerPublicKey
	a.cfg.IdentityPrivateKey = base64.RawStdEncoding.EncodeToString(a.priv)
	a.cfg.JoinToken = ""
	b, _ = json.MarshalIndent(a.cfg, "", "  ")
	tmp := a.cfgPath + ".tmp"
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	if e = os.Rename(tmp, a.cfgPath); e != nil {
		return e
	}
	pub, e := base64.RawStdEncoding.DecodeString(out.ServerPublicKey)
	if e != nil {
		return e
	}
	a.serverPub = ed25519.PublicKey(pub)
	return nil
}
func (a *Agent) cors(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Add("Vary", "Origin")
	if r.Header.Get("Origin") == "" || r.Header.Get("Origin") != a.cfg.AllowedOrigin {
		http.Error(w, "forbidden", 403)
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", a.cfg.AllowedOrigin)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	return true
}
func (a *Agent) preflight(w http.ResponseWriter, r *http.Request) {
	if a.cors(w, r) {
		w.WriteHeader(204)
	}
}
func (a *Agent) authorize(w http.ResponseWriter, r *http.Request, scope string) (grant.Claims, bool) {
	var z grant.Claims
	if !a.tlsReady.Load() {
		http.Error(w, "TLS unavailable", 503)
		return z, false
	}
	if !a.cors(w, r) {
		return z, false
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	c, e := grant.Verify(a.serverPub, tok, time.Now())
	if e != nil || c.NodeID != a.cfg.NodeID || !grant.HasScope(c, scope) {
		http.Error(w, "forbidden", 403)
		return z, false
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = strings.Trim(r.RemoteAddr, "[]")
	}
	ip, e := netip.ParseAddr(strings.Trim(host, "[]"))
	if e != nil || ip.Unmap().String() != c.ClientIP {
		http.Error(w, "forbidden", 403)
		return z, false
	}
	if r.Header.Get("Range") != "" {
		http.Error(w, "unsupported", 416)
		return z, false
	}
	return c, true
}
func (a *Agent) reserve(c grant.Claims, down, up int64) (func(), bool) {
	l := a.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	if l.day != today {
		l.day = today
		l.daily = 0
		l.byNonce = map[string]*usage{}
		l.active = map[string]int{}
	}
	if l.active == nil {
		l.active = map[string]int{}
	}
	if l.maxActive <= 0 {
		l.maxActive = 3
	}
	testKey := c.TestID
	if testKey == "" {
		testKey = c.Nonce
	}
	if l.active[testKey] == 0 && len(l.active) >= l.maxActive {
		return nil, false
	}
	u := l.byNonce[c.Nonce]
	if u == nil {
		u = &usage{Expires: c.Expires}
		l.byNonce[c.Nonce] = u
	}
	if u.Streams >= c.MaxStreams || u.Down+down > c.MaxDownloadBytes || u.Up+up > c.MaxUploadBytes || l.daily+down+up > l.limit {
		return nil, false
	}
	u.Streams++
	l.active[testKey]++
	u.Down += down
	u.Up += up
	l.daily += down + up
	if l.persistLocked() != nil {
		u.Streams--
		u.Down -= down
		u.Up -= up
		l.daily -= down + up
		l.active[testKey]--
		if l.active[testKey] <= 0 {
			delete(l.active, testKey)
		}
		return nil, false
	}
	return func() {
		l.mu.Lock()
		u.Streams--
		l.active[testKey]--
		if l.active[testKey] <= 0 {
			delete(l.active, testKey)
		}
		l.mu.Unlock()
	}, true
}

func (l *ledger) load() {
	b, err := os.ReadFile(l.path)
	if err != nil {
		return
	}
	var state quotaState
	if json.Unmarshal(b, &state) == nil && state.Day == time.Now().UTC().Format("2006-01-02") {
		l.day = state.Day
		l.daily = state.Bytes
	}
}

func (l *ledger) persistLocked() error {
	if l.path == "" {
		return nil
	}
	b, err := json.Marshal(quotaState{Day: l.day, Bytes: l.daily})
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}
func (a *Agent) probe(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorize(w, r, "probe"); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(204)
}
func (a *Agent) download(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorize(w, r, "download")
	if !ok {
		return
	}
	n, e := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64)
	if e != nil || n <= 0 || n > c.MaxDownloadBytes {
		http.Error(w, "invalid bytes", 400)
		return
	}
	done, ok := a.reserve(c, n, 0)
	if !ok {
		http.Error(w, "quota exceeded", 429)
		return
	}
	defer done()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Encoding", "identity")
	w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	w.Header().Set("Cache-Control", "no-store, no-transform")
	buf := make([]byte, 64*1024)
	for n > 0 {
		take := int64(len(buf))
		if take > n {
			take = n
		}
		if _, e = w.Write(buf[:take]); e != nil {
			return
		}
		n -= take
	}
}
func (a *Agent) upload(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorize(w, r, "upload")
	if !ok {
		return
	}
	n := r.ContentLength
	if n < 0 || n > c.MaxUploadBytes {
		http.Error(w, "invalid content length", 413)
		return
	}
	done, ok := a.reserve(c, 0, n)
	if !ok {
		http.Error(w, "quota exceeded", 429)
		return
	}
	defer done()
	read, e := io.Copy(io.Discard, io.LimitReader(r.Body, c.MaxUploadBytes+1))
	if e != nil || read != n {
		http.Error(w, "incomplete upload", 400)
		return
	}
	w.WriteHeader(204)
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

func (a *Agent) controlLoop(ctx context.Context) {
	back := time.Second
	for ctx.Err() == nil {
		if e := a.control(ctx); e != nil {
			slog.Warn("control connection lost", "error", e)
		}
		j := time.Duration(time.Now().UnixNano() % int64(back/2+1))
		select {
		case <-time.After(back + j):
		case <-ctx.Done():
			return
		}
		back *= 2
		if back > 60*time.Second {
			back = 60 * time.Second
		}
	}
}
func (a *Agent) control(ctx context.Context) error {
	u, e := url.Parse(strings.TrimRight(a.cfg.ServerURL, "/") + "/api/v1/agent/control")
	if e != nil {
		return e
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, _ := auth.RandomToken(12)
	msg := strings.Join([]string{ts, nonce, "GET", "/api/v1/agent/control"}, "\n")
	sig := ed25519.Sign(a.priv, []byte(msg))
	h := http.Header{"X-RouteGlass-Node": []string{a.cfg.NodeID}, "X-RouteGlass-Timestamp": []string{ts}, "X-RouteGlass-Nonce": []string{nonce}, "X-RouteGlass-Signature": []string{base64.RawURLEncoding.EncodeToString(sig)}}
	c, _, e := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPHeader: h})
	if e != nil {
		return e
	}
	defer c.CloseNow()
	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	errc := make(chan error, 1)
	go func() {
		for {
			var m controlMessage
			if e := wsjson.Read(ctx, c, &m); e != nil {
				errc <- e
				return
			}
			switch m.Type {
			case "ping":
				wsjson.Write(ctx, c, controlMessage{Type: "heartbeat", At: time.Now().Unix(), TLSReady: a.tlsReady.Load()})
			case "route":
				hops, _, e := routeexec.Run(ctx, m.Target)
				reply := controlMessage{Type: "route_result", ID: m.ID, Hops: hops}
				if e != nil {
					reply.Error = e.Error()
				}
				wsjson.Write(ctx, c, reply)
			}
		}
	}()
	for {
		select {
		case <-beat.C:
			if e := wsjson.Write(ctx, c, controlMessage{Type: "heartbeat", At: time.Now().Unix(), TLSReady: a.tlsReady.Load()}); e != nil {
				return e
			}
		case e := <-errc:
			return e
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func SaveExample(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path, b, 0600)
}
