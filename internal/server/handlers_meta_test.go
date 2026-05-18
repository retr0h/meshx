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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestEndpointHealth — GET /healthz. The probe is intentionally
// state-free so orchestration liveness checks can hit it before any
// radio attaches; it must succeed against a Server with a nil radios
// registry just as well as one with a fully populated one.
func TestEndpointHealth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		newSrv   func(t *testing.T) *httptest.Server
		wantBody string
	}{
		{
			name: "returns-status-ok-with-empty-registry",
			newSrv: func(t *testing.T) *httptest.Server {
				t.Helper()
				s := New(Config{Radios: NewRegistry()})
				srv := httptest.NewServer(s.http.Handler)
				t.Cleanup(srv.Close)
				return srv
			},
			wantBody: "ok",
		},
		{
			name: "returns-status-ok-with-radio-registered",
			newSrv: func(t *testing.T) *httptest.Server {
				t.Helper()
				srv, _ := radioOpsHarness(t)
				return srv
			},
			wantBody: "ok",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := tc.newSrv(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/healthz", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var body struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Status != tc.wantBody {
				t.Fatalf("status = %q, want %q", body.Status, tc.wantBody)
			}
		})
	}
}
