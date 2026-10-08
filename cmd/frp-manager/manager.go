package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Manager ties together configuration, process supervision and the web server.
type Manager struct {
	cfgPath string

	mu  sync.RWMutex
	cfg *ManagerConfig

	frps *Process
	frpc *Process

	sessions *sessionStore
	server   *http.Server
	serverMu sync.Mutex
}

// NewManager loads the manager configuration and prepares the two supervised
// processes. It does not start anything yet.
func NewManager(cfgPath string) (*Manager, error) {
	cfg, err := loadManagerConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	return &Manager{
		cfgPath:  cfgPath,
		cfg:      cfg,
		frps:     newProcess("frps", cfg.FrpsPath, cfg.FrpsConfig, cfg.WorkDir, int(cfg.LogMaxBytes)),
		frpc:     newProcess("frpc", cfg.FrpcPath, cfg.FrpcConfig, cfg.WorkDir, int(cfg.LogMaxBytes)),
		sessions: newSessionStore(),
	}, nil
}

// Run starts the web server and supervised processes and blocks until a
// termination signal is received.
func (m *Manager) Run() {
	m.mu.RLock()
	autoFrps := m.cfg.AutostartFrps
	autoFrpc := m.cfg.AutostartFrpc
	m.mu.RUnlock()
	if autoFrps {
		if err := m.frps.Start(); err != nil {
			log.Printf("autostart frps failed: %v", err)
		}
	}
	if autoFrpc {
		if err := m.frpc.Start(); err != nil {
			log.Printf("autostart frpc failed: %v", err)
		}
	}

	m.startServer()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
	m.Shutdown()
}

func (m *Manager) startServer() {
	m.serverMu.Lock()
	defer m.serverMu.Unlock()

	m.mu.RLock()
	addr := fmt.Sprintf("%s:%d", m.cfg.Web.Addr, m.cfg.Web.Port)
	m.mu.RUnlock()

	mux := m.routes()
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	m.server = srv
	go func() {
		log.Printf("frp-manager listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server error: %v", err)
		}
	}()
}

// restartServer rebinds the web server after the listen address changed.
func (m *Manager) restartServer() {
	m.serverMu.Lock()
	old := m.server
	m.serverMu.Unlock()
	if old != nil {
		_ = old.Close()
		time.Sleep(200 * time.Millisecond)
	}
	m.startServer()
}

// Shutdown gracefully stops the HTTP server and both processes.
func (m *Manager) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.serverMu.Lock()
	srv := m.server
	m.serverMu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(ctx)
	}
	m.frps.Stop()
	m.frpc.Stop()
}
