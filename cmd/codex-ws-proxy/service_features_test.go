package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultToken(t *testing.T) {
	t.Setenv("CODEX_WS_PROXY_DEFAULT_TOKEN", "local-test")
	p, err := newProxy("https://example.com/v1", false)
	require.NoError(t, err)
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Seen", r.Header.Get("Authorization")) })
	h, err := p.withDefaultToken(echo, defaultListen, "")
	require.NoError(t, err)
	for _, tc := range []struct {
		path, key, origin, host, want string
		status                        int
	}{
		{"/v1/codex/models", "", "", "localhost:28080", "Bearer local-test", 200},
		{"/v1/responses", "Bearer caller", "", "localhost", "Bearer caller", 200},
		{"/v1/models", "invalid", "", "localhost", "invalid", 200},
		{"/healthz", "", "", "localhost", "", 200},
		{"/v1/models", "", "https://example.com", "localhost", "", 403},
		{"/v1/models", "", "", "example.com", "", 403},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Host = tc.host
		if tc.key != "" {
			r.Header.Set("Authorization", tc.key)
		}
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, tc.status, w.Code)
		require.Equal(t, tc.want, w.Header().Get("Seen"))
	}
	_, err = p.withDefaultToken(echo, ":28080", "")
	require.Error(t, err)
	p, err = newProxy("ws://example.com/v1", false)
	require.NoError(t, err)
	_, err = p.withDefaultToken(echo, defaultListen, "")
	require.Error(t, err)
	file := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(file, []byte("file-key\n"), 0600))
	p, err = newProxy(defaultTarget, false)
	require.NoError(t, err)
	h, err = p.withDefaultToken(echo, defaultListen, file)
	require.NoError(t, err)
	r := httptest.NewRequest("GET", "http://localhost/v1/models", nil)
	r.RemoteAddr = "[::1]:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, "Bearer file-key", w.Header().Get("Seen"))
	require.NoError(t, os.Chmod(file, 0644))
	_, err = p.withDefaultToken(echo, defaultListen, file)
	require.Error(t, err)
}

func TestServiceUpdateReloadsChangedBinaryAndRetries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	calls := filepath.Join(dir, "calls")
	t.Setenv("CALLS", calls)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "launchctl"), []byte("#!/bin/sh\necho \"$1\" >> \"$CALLS\"\n[ \"$1\" != bootstrap ] || [ -z \"$FAIL_BOOTSTRAP\" ]\n"), 0700))
	path, err := launchAgentPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, writeServiceFile(path, []byte("原配置")))
	require.NoError(t, writeServiceFile(path+".sha256", []byte("旧版本")))
	t.Setenv("FAIL_BOOTSTRAP", "1")
	require.Error(t, checkServiceUpdate())
	previous, err := os.ReadFile(path + ".sha256")
	require.NoError(t, err)
	require.Equal(t, "旧版本", string(previous))
	t.Setenv("FAIL_BOOTSTRAP", "")
	require.NoError(t, checkServiceUpdate())
	require.NoError(t, checkServiceUpdate())
	log, err := os.ReadFile(calls)
	require.NoError(t, err)
	require.Equal(t, []string{"bootout", "bootstrap", "bootout", "bootstrap"}, strings.Fields(string(log)))
}
