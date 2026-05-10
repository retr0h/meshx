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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// TestEndpointUpdateConfig — PATCH /radios/{id}/config. Sparse PATCH:
// every field in the body is optional, fields the client supplies are
// dispatched as a single SetOwner (longname/shortname/is_licensed
// share an AdminMessage envelope) plus an optional SetBuzzer. Validation
// runs BEFORE Send so a doomed AdminMessage never reaches the radio.
func TestEndpointUpdateConfig(t *testing.T) {
	t.Parallel()

	type cmdMatcher func(t *testing.T, cmds []mdl.Command)

	hasOwner := func(longname, shortname string, licensed bool) cmdMatcher {
		return func(t *testing.T, cmds []mdl.Command) {
			t.Helper()
			for _, c := range cmds {
				if o, ok := c.(mdl.SetOwner); ok {
					if o.LongName != longname || o.ShortName != shortname || o.IsLicensed != licensed {
						t.Fatalf(
							"SetOwner = %+v; want {Long:%q Short:%q Licensed:%v}",
							o, longname, shortname, licensed,
						)
					}
					return
				}
			}
			t.Fatalf("no SetOwner dispatched; got %v", cmds)
		}
	}
	hasBuzzer := func(enabled bool) cmdMatcher {
		return func(t *testing.T, cmds []mdl.Command) {
			t.Helper()
			for _, c := range cmds {
				if b, ok := c.(mdl.SetBuzzer); ok {
					if b.Enabled != enabled {
						t.Fatalf("SetBuzzer.Enabled = %v, want %v", b.Enabled, enabled)
					}
					return
				}
			}
			t.Fatalf("no SetBuzzer dispatched; got %v", cmds)
		}
	}

	cases := []struct {
		name        string
		radioID     string
		body        string
		wantStatus  int
		wantApplied []string // sorted; empty when wantStatus != 202
		wantNCmds   int      // commands the pump should have seen
		wantMatch   []cmdMatcher
	}{
		{
			name:        "longname-only-dispatches-SetOwner-and-applies-longname",
			radioID:     "0xabcdef01",
			body:        `{"longname":"NEW LONGNAME"}`,
			wantStatus:  http.StatusAccepted,
			wantApplied: []string{"longname"},
			wantNCmds:   1,
			wantMatch:   []cmdMatcher{hasOwner("NEW LONGNAME", "", false)},
		},
		{
			name:        "buzzer-true-dispatches-SetBuzzer",
			radioID:     "0xabcdef01",
			body:        `{"buzzer":true}`,
			wantStatus:  http.StatusAccepted,
			wantApplied: []string{"buzzer"},
			wantNCmds:   1,
			wantMatch:   []cmdMatcher{hasBuzzer(true)},
		},
		{
			name:        "longname-plus-buzzer-coalesces-into-SetOwner-and-SetBuzzer",
			radioID:     "0xabcdef01",
			body:        `{"longname":"NEW","shortname":"NW","buzzer":false,"is_licensed":true}`,
			wantStatus:  http.StatusAccepted,
			wantApplied: []string{"buzzer", "is_licensed", "longname", "shortname"},
			wantNCmds:   2,
			wantMatch: []cmdMatcher{
				hasOwner("NEW", "NW", true),
				hasBuzzer(false),
			},
		},
		{
			name:        "empty-body-no-ops-applied-empty-no-commands",
			radioID:     "0xabcdef01",
			body:        `{}`,
			wantStatus:  http.StatusAccepted,
			wantApplied: []string{}, // no-op patch — applied stays empty
			wantNCmds:   0,
		},
		{
			name:       "longname-too-long-rejected-with-400-no-dispatch",
			radioID:    "0xabcdef01",
			body:       `{"longname":"this-name-is-deliberately-way-over-thirty-six-bytes-long"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "shortname-too-long-rejected-with-400-no-dispatch",
			radioID:    "0xabcdef01",
			body:       `{"shortname":"WAYTOOLONG"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty-longname-rejected-with-400-no-dispatch",
			radioID:    "0xabcdef01",
			body:       `{"longname":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown-radio-returns-404-no-dispatch",
			radioID:    "nope-no-such-radio",
			body:       `{"longname":"X"}`,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, pump := radioOpsHarness(t)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPatch,
				srv.URL+"/radios/"+tc.radioID+"/config",
				bytes.NewReader([]byte(tc.body)),
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}

			cmds := pump.snapshot()
			if got := len(cmds); got != tc.wantNCmds {
				t.Fatalf("dispatched %d commands, want %d", got, tc.wantNCmds)
			}
			for _, match := range tc.wantMatch {
				match(t, cmds)
			}

			if tc.wantStatus != http.StatusAccepted {
				return
			}
			var body UpdateConfigResult
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := append([]string{}, body.Applied...)
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(tc.wantApplied, ",") {
				t.Fatalf("applied = %v, want %v", got, tc.wantApplied)
			}
		})
	}
}

// TestEndpointRebootRadio — POST /radios/{id}/reboot. Dispatches a
// Reboot command with the requested seconds (default 5 to mirror the
// TUI grace), echoes the acknowledged delay back in the response.
func TestEndpointRebootRadio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		radioID     string
		body        string
		wantStatus  int
		wantSeconds int32 // delay echoed in the response
		wantCmd     mdl.Command
	}{
		{
			name:        "explicit-seconds-passed-through-to-Reboot-command",
			radioID:     "0xabcdef01",
			body:        `{"seconds":30}`,
			wantStatus:  http.StatusAccepted,
			wantSeconds: 30,
			wantCmd:     mdl.Reboot{Seconds: 30},
		},
		{
			name:        "missing-seconds-defaults-to-5",
			radioID:     "0xabcdef01",
			body:        `{}`,
			wantStatus:  http.StatusAccepted,
			wantSeconds: 5,
			wantCmd:     mdl.Reboot{Seconds: 5},
		},
		{
			name:        "zero-seconds-defaults-to-5",
			radioID:     "0xabcdef01",
			body:        `{"seconds":0}`,
			wantStatus:  http.StatusAccepted,
			wantSeconds: 5,
			wantCmd:     mdl.Reboot{Seconds: 5},
		},
		{
			name:       "negative-seconds-rejected-with-422",
			radioID:    "0xabcdef01",
			body:       `{"seconds":-1}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "seconds-over-max-rejected-with-422",
			radioID:    "0xabcdef01",
			body:       `{"seconds":99999}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unknown-radio-returns-404-no-dispatch",
			radioID:    "nope-no-such-radio",
			body:       `{"seconds":5}`,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, pump := radioOpsHarness(t)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/radios/"+tc.radioID+"/reboot",
				bytes.NewReader([]byte(tc.body)),
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}

			cmds := pump.snapshot()
			if tc.wantStatus != http.StatusAccepted {
				if got := len(cmds); got != 0 {
					t.Fatalf("dispatched %d commands on non-202; pump must be untouched", got)
				}
				return
			}
			if got := len(cmds); got != 1 {
				t.Fatalf("dispatched %d commands, want 1", got)
			}
			if cmds[0] != tc.wantCmd {
				t.Fatalf("dispatched %#v, want %#v", cmds[0], tc.wantCmd)
			}
			var body RebootResult
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Seconds != tc.wantSeconds {
				t.Fatalf("seconds = %d, want %d", body.Seconds, tc.wantSeconds)
			}
		})
	}
}
