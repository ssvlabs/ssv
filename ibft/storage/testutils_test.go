package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// moduleCacheDirFromEnv follows the go command: GOMODCACHE, else pkg/mod under the first GOPATH entry; with
// neither, `go env` decides.
func TestModuleCacheDirFromEnv(t *testing.T) {
	list := func(paths ...string) string { return strings.Join(paths, string(os.PathListSeparator)) }
	tests := []struct {
		name               string
		goModCache, goPath string
		want               string
		ok                 bool
	}{
		{name: "GOMODCACHE wins", goModCache: "/cache", goPath: "/gopath", want: "/cache", ok: true},
		{name: "single GOPATH entry", goPath: "/gopath", want: filepath.Join("/gopath", "pkg", "mod"), ok: true},
		{name: "first of several GOPATH entries", goPath: list("/first", "/second"), want: filepath.Join("/first", "pkg", "mod"), ok: true},
		{name: "empty first GOPATH entry", goPath: list("", "/second")},
		{name: "neither set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := moduleCacheDirFromEnv(tt.goModCache, tt.goPath)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
