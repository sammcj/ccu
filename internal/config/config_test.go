package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ParseFlags itself is not exercised here: it registers flags on the global
// flag.CommandLine and calls flag.Parse against os.Args (which under `go test`
// carries -test.* flags), and it may reach the macOS keychain via
// oauth.DetectPlan. Testing the extractable pure helpers instead keeps these
// tests hermetic and repeatable. resolveAPIPort covers the CLI-flag/env-var
// precedence and invalid-port fallback logic used by ParseFlags.

func TestResolveAPIPort(t *testing.T) {
	const flagDefault = 19840

	tests := []struct {
		name     string
		explicit bool
		flagVal  int
		env      string
		want     int
	}{
		{"explicit flag beats env", true, 12345, "22222", 12345},
		{"explicit flag beats invalid env", true, 12345, "not-a-port", 12345},
		{"env used when flag not set", false, flagDefault, "22222", 22222},
		{"invalid env falls back to default", false, flagDefault, "not-a-port", flagDefault},
		{"empty env uses default", false, flagDefault, "", flagDefault},
		{"negative env value passes through", false, flagDefault, "-1", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAPIPort(tt.explicit, tt.flagVal, tt.env)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseCIDRList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"single entry", "192.168.1.0/24", []string{"192.168.1.0/24"}},
		{"multiple entries", "10.0.0.0/8,192.168.1.0/24", []string{"10.0.0.0/8", "192.168.1.0/24"}},
		{"trims whitespace", " 10.0.0.0/8 , 192.168.1.0/24 ", []string{"10.0.0.0/8", "192.168.1.0/24"}},
		{"drops empty segments", "10.0.0.0/8,,192.168.1.0/24,", []string{"10.0.0.0/8", "192.168.1.0/24"}},
		// parseCIDRList only splits and trims; validation happens later in api.New,
		// so a malformed entry is passed through unchanged.
		{"invalid entry passes through", "not-a-cidr", []string{"not-a-cidr"}},
		{"empty string", "", []string{}},
		{"whitespace only", "   ", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCIDRList(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReadTokenFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		write   bool
		want    string
	}{
		{"trims trailing newline", "secret-token\n", true, "secret-token"},
		{"trims surrounding whitespace", "  secret-token  \n", true, "secret-token"},
		{"empty file yields empty token", "", true, ""},
		{"missing file yields empty token", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			if tt.write {
				dir := filepath.Join(home, ".ccu")
				require.NoError(t, os.MkdirAll(dir, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".api_token"), []byte(tt.content), 0o600))
			}

			assert.Equal(t, tt.want, readTokenFile())
		})
	}
}
