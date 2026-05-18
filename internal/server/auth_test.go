// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIsLoopbackBind enumerates every bind-address shape the cmd
// layer might pass: bare port, loopback IPv4, loopback IPv6,
// localhost hostname, all-interfaces IP, public IP, garbled. The
// auth policy in resolveAuthToken hinges on this classification —
// a wrong call here exposes the radio unauthenticated.
func TestIsLoopbackBind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
		want bool
	}{
		{"loopback-ipv4-with-port", "127.0.0.1:4404", true},
		{"loopback-ipv6-with-port", "[::1]:4404", true},
		{"localhost-hostname", "localhost:4404", true},
		{"all-interfaces-bare-port", ":4404", false},
		{"all-interfaces-zero", "0.0.0.0:4404", false},
		{"public-ipv4", "10.0.0.5:4404", false},
		{"unknown-hostname", "myhost.lan:4404", false},
		{"malformed-no-port", "127.0.0.1", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsLoopbackBind(tc.addr); got != tc.want {
				t.Fatalf("IsLoopbackBind(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

// TestLoadAuthToken pins the file-handling contract: empty path is
// a no-op, missing file generates+writes (and survives the round
// trip), existing file reads (with whitespace trimmed), empty file
// errors, unreadable file errors.
func TestLoadAuthToken(t *testing.T) {
	t.Parallel()

	t.Run("empty-path-returns-empty", func(t *testing.T) {
		got, err := LoadAuthToken("")
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got != "" {
			t.Fatalf("token = %q, want empty", got)
		}
	})

	t.Run("missing-file-generates-and-persists", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "subdir", "token")
		first, err := LoadAuthToken(path)
		if err != nil {
			t.Fatalf("first load: %v", err)
		}
		if len(first) != authTokenLen*2 {
			t.Fatalf("token len = %d, want %d hex chars", len(first), authTokenLen*2)
		}
		// Second load reads the same value off disk — generator does
		// not run twice, otherwise the daemon would invalidate every
		// active client by restarting.
		second, err := LoadAuthToken(path)
		if err != nil {
			t.Fatalf("second load: %v", err)
		}
		if first != second {
			t.Fatalf("first %q != second %q (token must persist)", first, second)
		}
		// File perms must be 0o600 — the file IS the trust anchor.
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("perm = %o, want 0600", perm)
		}
	})

	t.Run("trims-whitespace-from-existing-file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "token")
		if err := os.WriteFile(path, []byte("  abc123\n\n"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		got, err := LoadAuthToken(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got != "abc123" {
			t.Fatalf("token = %q, want \"abc123\"", got)
		}
	})

	t.Run("empty-file-errors", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "token")
		if err := os.WriteFile(path, []byte("   \n\t  "), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := LoadAuthToken(path); err == nil {
			t.Fatalf("err = nil, want error for empty token file")
		}
	})
}

// authHarness wires a Server with a known token and returns an
// httptest server backed by the same handler chain production uses.
// Auth-bearing requests go through every middleware, including
// authMiddleware.
func authHarness(t *testing.T, token string) *httptest.Server {
	t.Helper()
	s := New(Config{Radios: NewRegistry(), AuthToken: token})
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return srv
}

// TestAuthMiddlewareGate is the single behavior-as-table that
// covers every meaningful combination of (server has token,
// request has header, header is correct). Adding a new policy
// (token rotation, key id, etc.) is one row.
func TestAuthMiddlewareGate(t *testing.T) {
	t.Parallel()
	const goodToken = "the-correct-token-value"

	cases := []struct {
		name        string
		serverToken string
		path        string
		authHeader  string
		wantStatus  int
	}{
		{
			name:        "no-token-configured-anyone-passes",
			serverToken: "",
			path:        "/radios",
			authHeader:  "",
			wantStatus:  http.StatusOK,
		},
		{
			name:        "valid-bearer-token-passes",
			serverToken: goodToken,
			path:        "/radios",
			authHeader:  "Bearer " + goodToken,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "missing-header-rejects-401",
			serverToken: goodToken,
			path:        "/radios",
			authHeader:  "",
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "wrong-token-rejects-401",
			serverToken: goodToken,
			path:        "/radios",
			authHeader:  "Bearer not-the-right-token",
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "missing-bearer-prefix-rejects-401",
			serverToken: goodToken,
			path:        "/radios",
			authHeader:  goodToken, // no `Bearer ` prefix
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "wrong-scheme-rejects-401",
			serverToken: goodToken,
			path:        "/radios",
			authHeader:  "Basic dXNlcjpwYXNz",
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "healthz-exempt-even-without-token",
			serverToken: goodToken,
			path:        "/healthz",
			authHeader:  "",
			wantStatus:  http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := authHarness(t, tc.serverToken)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}
