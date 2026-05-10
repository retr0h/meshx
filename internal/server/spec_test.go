// Copyright (c) 2026 John Dewey

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAPISpecMatchesVendoredCopy is the drift gate — spins up the
// real Server in-process, fetches its OpenAPI 3.0 spec, and asserts
// the on-disk vendored copy at internal/sdk/gen/api.yaml matches
// byte-for-byte. A PR that changes a route, handler, or request /
// response type without regenerating the SDK will fail here with
// "spec drift — run `just generate`."
//
// Catches the most common SDK-rot failure mode: someone tweaks a
// schema field, doesn't regen, the generated client.gen.go grows
// stale, downstream SDK consumers (this repo's TUI in remote mode,
// future external clients) silently see incomplete shapes.
//
// The breaking-change side of API hygiene (was a field removed?
// did an enum narrow?) lives in the CI workflow — see
// `.github/workflows/go.yml` `oasdiff` step.
func TestAPISpecMatchesVendoredCopy(t *testing.T) {
	t.Parallel()

	// On-disk vendored copy. Path is relative to this test file's
	// package — internal/sdk/gen/api.yaml.
	specPath := filepath.Join("..", "sdk", "gen", "api.yaml")
	onDisk, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read vendored spec %q: %v", specPath, err)
	}

	s := New(Config{Radios: NewRegistry()})
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/openapi-3.0.yaml", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch spec: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	live, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if string(onDisk) != string(live) {
		// Write the live copy to a temp file so the developer can
		// `diff` it locally without needing to spawn the daemon
		// themselves. The repro recipe is `just generate`.
		tmp := filepath.Join(t.TempDir(), "live-api.yaml")
		if writeErr := os.WriteFile(tmp, live, 0o600); writeErr == nil {
			t.Fatalf(
				"vendored spec drift — on-disk %s does not match the daemon's live spec.\n"+
					"  fresh dump: %s\n"+
					"  fix: run `just generate` to refresh internal/sdk/gen/api.yaml + client.gen.go and commit",
				specPath, tmp,
			)
		}
		t.Fatalf("vendored spec drift — run `just generate` to regenerate %s", specPath)
	}
}
