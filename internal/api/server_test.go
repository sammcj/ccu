package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(cfg models.APIConfig) *Server {
	return New(cfg)
}

// getFreePort picks an available TCP port on loopback.
func getFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestServer_ConcurrentRequests(t *testing.T) {
	snapshot := []byte(`{"plan":"max5"}`)
	s := newTestServer(models.APIConfig{})
	s.UpdateSnapshot(snapshot)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			rr := httptest.NewRecorder()
			s.handleStatus(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
		}()
	}

	wg.Wait()
}

func TestServer_StartStop(t *testing.T) {
	port := getFreePort(t)
	cfg := models.APIConfig{
		Port:     port,
		BindAddr: "127.0.0.1",
	}

	s := newTestServer(cfg)
	s.UpdateSnapshot([]byte(`{"plan":"max5"}`))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start(ctx)
	}()

	// Wait for server to start
	addr := fmt.Sprintf("http://127.0.0.1:%d/api/status", port)
	var resp *http.Response
	var err error
	for i := 0; i < 20; i++ {
		resp, err = http.Get(addr) //nolint:noctx
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Cancel context and wait for clean shutdown
	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}

func TestServer_AuthToken(t *testing.T) {
	s := newTestServer(models.APIConfig{Token: "correct-token"})
	s.UpdateSnapshot([]byte(`{"plan":"max5"}`))

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"no token", "", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong", http.StatusUnauthorized},
		{"correct token", "Bearer correct-token", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()
			s.handleStatus(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestServer_NoSnapshotReturns503(t *testing.T) {
	s := newTestServer(models.APIConfig{})
	// No UpdateSnapshot call

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	s.handleStatus(rr, req)

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	body, _ := io.ReadAll(rr.Body)
	assert.Contains(t, string(body), "no data")
}

func TestServer_IPAllowlist(t *testing.T) {
	cfg := models.APIConfig{
		AllowedCIDRs: []string{"192.168.1.0/24"},
	}
	s := newTestServer(cfg)
	s.UpdateSnapshot([]byte(`{"plan":"max5"}`))

	tests := []struct {
		name       string
		remoteAddr string
		wantStatus int
	}{
		{"allowed IP", "192.168.1.42:12345", http.StatusOK},
		{"denied IP", "10.0.0.1:12345", http.StatusForbidden},
		{"denied external", "8.8.8.8:12345", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			req.RemoteAddr = tt.remoteAddr
			rr := httptest.NewRecorder()
			s.handleStatus(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestServer_IPAllowlistEmpty(t *testing.T) {
	cfg := models.APIConfig{
		AllowedCIDRs: []string{},
	}
	s := newTestServer(cfg)
	s.UpdateSnapshot([]byte(`{"plan":"max5"}`))

	// Any remote address should be allowed when allowlist is empty
	for _, addr := range []string{"1.2.3.4:5678", "192.168.1.1:9999", "10.0.0.1:1234"} {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.RemoteAddr = addr
		rr := httptest.NewRecorder()
		s.handleStatus(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "addr %s should be allowed", addr)
	}
}
