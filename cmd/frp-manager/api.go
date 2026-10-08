package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ---------- session store ----------

type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{tokens: map[string]time.Time{}}
}

func (s *sessionStore) create() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.tokens[tok] = time.Now()
	s.mu.Unlock()
	return tok
}

func (s *sessionStore) valid(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.tokens[tok]
	return ok
}

func (s *sessionStore) delete(tok string) {
	s.mu.Lock()
	delete(s.tokens, tok)
	s.mu.Unlock()
}

// ---------- http helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSONBody(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	return dec.Decode(v)
}

const sessionCookie = "frpm_session"

func (m *Manager) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || !m.sessions.valid(c.Value) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (m *Manager) process(name string) *Process {
	if name == "frpc" {
		return m.frpc
	}
	return m.frps
}

// ---------- routes ----------

func (m *Manager) routes() http.Handler {
	mux := http.NewServeMux()

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("POST /api/login", m.handleLogin)
	mux.HandleFunc("POST /api/logout", m.requireAuth(m.handleLogout))
	mux.HandleFunc("GET /api/status", m.requireAuth(m.handleStatus))
	mux.HandleFunc("GET /api/version", m.requireAuth(m.handleVersion))

	mux.HandleFunc("GET /api/schema/server", m.requireAuth(m.handleSchemaServer))
	mux.HandleFunc("GET /api/schema/client", m.requireAuth(m.handleSchemaClient))
	mux.HandleFunc("GET /api/schema/proxy/{type}", m.requireAuth(m.handleSchemaProxy))
	mux.HandleFunc("GET /api/schema/visitor/{type}", m.requireAuth(m.handleSchemaVisitor))
	mux.HandleFunc("GET /api/schema/plugin/{type}", m.requireAuth(m.handleSchemaClientPlugin))
	mux.HandleFunc("GET /api/schema/visitor-plugin/{type}", m.requireAuth(m.handleSchemaVisitorPlugin))

	mux.HandleFunc("GET /api/config/server", m.requireAuth(m.handleGetServerConfig))
	mux.HandleFunc("PUT /api/config/server", m.requireAuth(m.handlePutServerConfig))
	mux.HandleFunc("GET /api/config/client", m.requireAuth(m.handleGetClientConfig))
	mux.HandleFunc("PUT /api/config/client", m.requireAuth(m.handlePutClientConfig))

	mux.HandleFunc("POST /api/process/{name}/{action}", m.requireAuth(m.handleProcess))
	mux.HandleFunc("GET /api/log/{name}", m.requireAuth(m.handleLog))

	mux.HandleFunc("GET /api/settings", m.requireAuth(m.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", m.requireAuth(m.handlePutSettings))

	mux.HandleFunc("GET /api/update/check", m.requireAuth(m.handleUpdateCheck))
	mux.HandleFunc("POST /api/update/apply", m.requireAuth(m.handleUpdateApply))
	mux.HandleFunc("GET /api/example/{kind}", m.requireAuth(m.handleExample))

	return mux
}

// ---------- handlers ----------

func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := readJSONBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	m.mu.RLock()
	user, pass := m.cfg.Web.User, m.cfg.Web.Password
	m.mu.RUnlock()
	if body.User != user || body.Password != pass {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	tok := m.sessions.create()
	// The manager serves plain HTTP (no TLS), so Secure must be false here;
	// if you terminate TLS at a reverse proxy, set Secure=true behind it.
	//nolint:gosec // Secure is intentionally false for the HTTP-only panel
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Manager) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		m.sessions.delete(c.Value)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Manager) handleStatus(w http.ResponseWriter, r *http.Request) {
	frps := m.frps.Status()
	frpc := m.frpc.Status()
	m.mu.RLock()
	cfg := *m.cfg
	m.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"managerVersion": managerVersion,
		"os":             runtime.GOOS,
		"arch":           runtime.GOARCH,
		"platform":       runtime.GOOS + "_" + runtime.GOARCH,
		"frps":           frps,
		"frpc":           frpc,
		"frpsVersion":    localBinaryVersion(cfg.FrpsPath),
		"frpcVersion":    localBinaryVersion(cfg.FrpcPath),
		"webPort":        cfg.Web.Port,
		"mirror":         cfg.Mirror,
		"autostartFrps":  cfg.AutostartFrps,
		"autostartFrpc":  cfg.AutostartFrpc,
	})
}

func (m *Manager) handleVersion(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	cfg := *m.cfg
	m.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]string{
		"manager": managerVersion,
		"frps":    localBinaryVersion(cfg.FrpsPath),
		"frpc":    localBinaryVersion(cfg.FrpcPath),
	})
}

func (m *Manager) handleSchemaServer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, serverSchema())
}

func (m *Manager) handleSchemaClient(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, clientCommonSchema())
}

func (m *Manager) handleSchemaProxy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, proxySchema(r.PathValue("type")))
}

func (m *Manager) handleSchemaVisitor(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, visitorSchema(r.PathValue("type")))
}

func (m *Manager) handleSchemaClientPlugin(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, clientPluginSchema(r.PathValue("type")))
}

func (m *Manager) handleSchemaVisitorPlugin(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, visitorPluginSchema(r.PathValue("type")))
}

func (m *Manager) handleGetServerConfig(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	path := m.cfg.FrpsConfig
	m.mu.RUnlock()
	cfg, err := loadServerConfig(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": cfg})
}

func (m *Manager) handlePutServerConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Config map[string]any `json:"config"`
	}
	if err := readJSONBody(r, &body); err != nil || body.Config == nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	m.mu.RLock()
	path := m.cfg.FrpsConfig
	m.mu.RUnlock()
	if err := saveServerConfig(path, body.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Manager) handleGetClientConfig(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	path := m.cfg.FrpcConfig
	m.mu.RUnlock()
	cfg, err := loadClientConfig(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": cfg})
}

func (m *Manager) handlePutClientConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Config map[string]any `json:"config"`
	}
	if err := readJSONBody(r, &body); err != nil || body.Config == nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	m.mu.RLock()
	path := m.cfg.FrpcConfig
	m.mu.RUnlock()
	if err := saveClientConfig(path, body.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Manager) handleProcess(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	action := r.PathValue("action")
	p := m.process(name)
	if p == nil {
		writeErr(w, http.StatusNotFound, "unknown process")
		return
	}
	var err error
	switch action {
	case "start":
		err = p.Start()
	case "stop":
		p.Stop()
	case "restart":
		err = p.Restart()
	default:
		writeErr(w, http.StatusBadRequest, "unknown action")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p.Status())
}

func (m *Manager) handleLog(w http.ResponseWriter, r *http.Request) {
	p := m.process(r.PathValue("name"))
	if p == nil {
		writeErr(w, http.StatusNotFound, "unknown process")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, p.Log())
}

type settingsPayload struct {
	WebAddr       string `json:"webAddr"`
	WebPort       int    `json:"webPort"`
	WebUser       string `json:"webUser"`
	WebPassword   string `json:"webPassword"`
	FrpsPath      string `json:"frpsPath"`
	FrpcPath      string `json:"frpcPath"`
	FrpsConfig    string `json:"frpsConfig"`
	FrpcConfig    string `json:"frpcConfig"`
	WorkDir       string `json:"workDir"`
	Mirror        string `json:"mirror"`
	AutostartFrps bool   `json:"autostartFrps"`
	AutostartFrpc bool   `json:"autostartFrpc"`
}

func (m *Manager) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	c := *m.cfg
	m.mu.RUnlock()
	writeJSON(w, http.StatusOK, settingsPayload{
		WebAddr:       c.Web.Addr,
		WebPort:       c.Web.Port,
		WebUser:       c.Web.User,
		WebPassword:   c.Web.Password,
		FrpsPath:      c.FrpsPath,
		FrpcPath:      c.FrpcPath,
		FrpsConfig:    c.FrpsConfig,
		FrpcConfig:    c.FrpcConfig,
		WorkDir:       c.WorkDir,
		Mirror:        c.Mirror,
		AutostartFrps: c.AutostartFrps,
		AutostartFrpc: c.AutostartFrpc,
	})
}

func (m *Manager) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var p settingsPayload
	if err := readJSONBody(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if p.WebPort <= 0 || p.WebPort > 65535 {
		writeErr(w, http.StatusBadRequest, "invalid web port")
		return
	}
	if p.WebUser == "" {
		writeErr(w, http.StatusBadRequest, "web user cannot be empty")
		return
	}

	m.mu.Lock()
	base := filepath.Dir(m.cfgPath)
	oldPort := m.cfg.Web.Port
	oldAddr := m.cfg.Web.Addr
	oldFrpsPath := m.cfg.FrpsPath
	oldFrpcPath := m.cfg.FrpcPath
	oldFrpsConfig := m.cfg.FrpsConfig
	oldFrpcConfig := m.cfg.FrpcConfig
	oldWorkDir := m.cfg.WorkDir

	m.cfg.Web.Addr = p.WebAddr
	m.cfg.Web.Port = p.WebPort
	m.cfg.Web.User = p.WebUser
	if p.WebPassword != "" {
		m.cfg.Web.Password = p.WebPassword
	}
	m.cfg.FrpsPath = resolvePath(base, p.FrpsPath)
	m.cfg.FrpcPath = resolvePath(base, p.FrpcPath)
	m.cfg.FrpsConfig = resolvePath(base, p.FrpsConfig)
	m.cfg.FrpcConfig = resolvePath(base, p.FrpcConfig)
	m.cfg.WorkDir = resolvePath(base, p.WorkDir)
	m.cfg.Mirror = strings.TrimRight(strings.TrimSpace(p.Mirror), "/")
	m.cfg.AutostartFrps = p.AutostartFrps
	m.cfg.AutostartFrpc = p.AutostartFrpc
	cfgCopy := *m.cfg
	m.mu.Unlock()

	if err := saveManagerConfig(m.cfgPath, &cfgCopy); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If any process path changed, stop the old processes and recreate the
	// supervisors so they point at the new locations.
	if oldFrpsPath != cfgCopy.FrpsPath || oldFrpcPath != cfgCopy.FrpcPath ||
		oldFrpsConfig != cfgCopy.FrpsConfig || oldFrpcConfig != cfgCopy.FrpcConfig ||
		oldWorkDir != cfgCopy.WorkDir {
		m.frps.Stop()
		m.frpc.Stop()
		m.frps = newProcess("frps", cfgCopy.FrpsPath, cfgCopy.FrpsConfig, cfgCopy.WorkDir, int(cfgCopy.LogMaxBytes))
		m.frpc = newProcess("frpc", cfgCopy.FrpcPath, cfgCopy.FrpcConfig, cfgCopy.WorkDir, int(cfgCopy.LogMaxBytes))
	}

	if oldPort != p.WebPort || oldAddr != p.WebAddr {
		go m.restartServer()
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Manager) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	mirror := m.cfg.Mirror
	frpsPath := m.cfg.FrpsPath
	m.mu.RUnlock()

	current := localBinaryVersion(frpsPath)
	rel, err := fetchRelease(mirror)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"current":  current,
			"platform": runtime.GOOS + "_" + runtime.GOARCH,
			"error":    err.Error(),
		})
		return
	}
	asset, ver, err := findAsset(rel)
	resp := map[string]any{
		"current":  current,
		"latest":   "v" + ver,
		"upToDate": current == ver,
		"platform": runtime.GOOS + "_" + runtime.GOARCH,
	}
	if err != nil {
		resp["error"] = err.Error()
	} else {
		resp["assetName"] = asset.Name
		resp["assetSize"] = asset.Size
	}
	writeJSON(w, http.StatusOK, resp)
}

func (m *Manager) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	mirror := m.cfg.Mirror
	m.mu.RUnlock()
	rel, err := fetchRelease(mirror)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch latest release: %v", err))
		return
	}
	res, err := m.applyUpdate(rel)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (m *Manager) handleExample(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	var name string
	switch kind {
	case "server":
		name = "frps_full_example.toml"
	case "client":
		name = "frpc_full_example.toml"
	default:
		writeErr(w, http.StatusNotFound, "unknown example")
		return
	}
	data, err := examplesFS.ReadFile("examples/" + name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}
