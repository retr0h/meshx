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
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// auth.go owns the bearer-token gate the daemon enforces when bound
// to anything other than loopback. Single-user shape: one shared
// secret in a file, clients send `Authorization: Bearer <token>`,
// constant-time comparison decides 200 or 401. Health probes
// (/healthz) skip the gate so orchestration liveness checks don't
// need credentials.
//
// Loopback (127.0.0.1, ::1) is the auth boundary — process
// ownership on the same host is the only trust anchor. The moment
// the bind address isn't loopback, a token is required (or the
// operator explicitly opts out via --auth-disabled).

// authTokenLen is the hex-byte length of a freshly-minted token —
// 32 bytes of crypto/rand → 64 hex chars. Larger than typical UUIDs
// (which are 16 bytes) because the token is the entire trust anchor;
// no second factor, no rotation policy yet.
const authTokenLen = 32

// authBearerPrefix is the case-insensitive header prefix the
// middleware recognizes. RFC 6750 §2.1 — `Bearer <credentials>`.
const authBearerPrefix = "Bearer "

// authExemptPaths skip the gate even when auth is enabled. Liveness
// probes shouldn't have to know the token; an unauthenticated probe
// is a smaller risk than a probe that can't run at all.
var authExemptPaths = map[string]struct{}{
	"/healthz": {},
}

// LoadAuthToken resolves the token-file pathway used at server
// startup. The file is the source of truth — generated lazily on
// first run when missing, read otherwise. Empty path returns "" with
// no error (caller decides whether that's a fatal misconfiguration).
//
// Trims surrounding whitespace from the file body so a token-file
// edited by hand with a trailing newline still works.
func LoadAuthToken(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	body, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		token, err := generateAuthToken()
		if err != nil {
			return "", fmt.Errorf("generate token: %w", err)
		}
		if err := writeAuthToken(path, token); err != nil {
			return "", fmt.Errorf("write token to %s: %w", path, err)
		}
		return token, nil
	case err != nil:
		return "", fmt.Errorf("read token from %s: %w", path, err)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}

// generateAuthToken returns authTokenLen random bytes hex-encoded.
func generateAuthToken() (string, error) {
	var b [authTokenLen]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// writeAuthToken persists token to path with 0o600 perms (owner
// read/write only) and creates the parent directory if missing. The
// file IS the trust anchor; group / other access would be a leak.
func writeAuthToken(path, token string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}

// IsLoopbackBind reports whether the bind address points at the
// loopback interface — 127.0.0.1, ::1, or the literal "localhost".
// Empty host (":4404") binds to ALL interfaces, which is NOT
// loopback. Any error parsing the address falls through as
// "not loopback" so the caller defaults to the safer side.
func IsLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// addr might be just ":4404"; SplitHostPort fails on "no
		// port" but succeeds with host="" on ":4404" — the err
		// path here is genuine malformed input, treat as
		// non-loopback.
		return false
	}
	if host == "" {
		// :PORT binds to all interfaces — not loopback.
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Hostname that's not "localhost" — could be anything.
		// Conservatively treat as non-loopback so the operator has
		// to opt out explicitly via --auth-disabled.
		return false
	}
	return ip.IsLoopback()
}

// authMiddleware compares the request's Authorization header
// against the configured token using crypto/subtle.ConstantTimeCompare
// to avoid timing-side-channel leaks. Empty configured token =
// passthrough (loopback default). Exempt paths bypass entirely.
func (s *Server) authMiddleware(ctx huma.Context, next func(huma.Context)) {
	if s.authToken == "" {
		next(ctx)
		return
	}
	if _, ok := authExemptPaths[ctx.URL().Path]; ok {
		next(ctx)
		return
	}
	header := ctx.Header("Authorization")
	if !strings.HasPrefix(header, authBearerPrefix) {
		_ = huma.WriteErr(s.api, ctx, 401, "missing or malformed Authorization header")
		return
	}
	got := strings.TrimSpace(strings.TrimPrefix(header, authBearerPrefix))
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.authToken)) != 1 {
		_ = huma.WriteErr(s.api, ctx, 401, "invalid bearer token")
		return
	}
	next(ctx)
}
