package api

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

// Server is an optional embedded HTTP API server that serves a JSON snapshot
// of CCU's current usage state. It is safe for concurrent access.
type Server struct {
	mu         sync.RWMutex
	snapshot   []byte
	snapshotAt time.Time
	config     models.APIConfig
	allowedNets []*net.IPNet
}

// New creates a new Server with the given configuration.
// CIDR ranges are parsed eagerly so any configuration errors are caught at startup.
func New(cfg models.APIConfig) *Server {
	s := &Server{config: cfg}
	for _, cidr := range cfg.AllowedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Printf("api: ignoring invalid CIDR %q: %v", cidr, err)
			continue
		}
		s.allowedNets = append(s.allowedNets, ipNet)
	}
	return s
}

// UpdateSnapshot replaces the cached JSON snapshot. Safe to call from any goroutine.
func (s *Server) UpdateSnapshot(data []byte) {
	s.mu.Lock()
	s.snapshot = data
	s.snapshotAt = time.Now()
	s.mu.Unlock()
}

// Start listens on the configured address and serves requests until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	if len(s.allowedNets) == 0 && s.config.Token == "" {
		log.Printf("api: WARNING – server is unauthenticated and open to all hosts (consider setting -api-token or -api-allow)")
	}

	addr := fmt.Sprintf("%s:%d", s.config.BindAddr, s.config.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.handleStatus)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("api: listen %s: %w", addr, err)
	}

	log.Printf("api: listening on %s", addr)

	// Shut down gracefully when ctx is cancelled
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("api: shutdown error: %v", err)
		}
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("api: serve: %w", err)
	}
	return nil
}

// handleStatus serves the cached JSON snapshot with optional auth and IP allowlist checks.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// IP allowlist check (before auth to avoid leaking that auth exists)
	if len(s.allowedNets) > 0 {
		if !s.isAllowedIP(r.RemoteAddr) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Bearer token check
	if s.config.Token != "" {
		token := extractBearerToken(r)
		if token != s.config.Token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	s.mu.RLock()
	data := s.snapshot
	s.mu.RUnlock()

	if len(data) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"no data"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=5")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// isAllowedIP returns true if the remote address falls within any configured CIDR.
func (s *Server) isAllowedIP(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// RemoteAddr without port (unusual but possible in tests)
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, ipNet := range s.allowedNets {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// extractBearerToken returns the token from an "Authorization: Bearer <token>" header.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return auth[len(prefix):]
	}
	return ""
}
