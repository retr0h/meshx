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

package cmd

// Tests for `meshx client …` subcommands' testable cores. Each
// run<Verb> function takes a *gen.ClientWithResponses + an
// io.Writer so we can drive it against an httptest harness and
// assert on the rendered output without spinning up cobra/viper.
//
// The harness uses internal/server's real *Server so the wire path
// goes through Huma's router + middleware — same code the daemon
// runs.

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/retr0h/meshx/internal/radio"
	"github.com/retr0h/meshx/internal/sdk/gen"
	"github.com/retr0h/meshx/internal/server"
)

// clientHarness builds a real *server.Server with the given attached
// radios, an httptest.Server in front of it, and returns a wired SDK
// client pointed at that server. Empty radios slice produces an
// empty registry — useful for the "no radios attached" branch.
func clientHarness(
	t *testing.T,
	radios ...*radio.Session,
) *gen.ClientWithResponses {
	t.Helper()
	s := server.New(server.Config{Radios: server.NewRegistry()})
	for _, sess := range radios {
		s.Drivers().Add(sess.State.RadioID, sess)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c, err := gen.NewClientWithResponses(srv.URL)
	if err != nil {
		t.Fatalf("build SDK client: %v", err)
	}
	return c
}

// fakeRadio is a *radio.Session pre-seeded with the canonical-shape
// fields the /radios endpoints project. Test rows mutate one of
// these and hand it to clientHarness.
//
// MyNodeNum is kept under int32 max because the generated SDK
// (RadioSummary.MyNodeNum int32) narrows uint32 on decode — a
// pre-existing spec bug that needs a separate fix. Real radios with
// node nums > 2^31-1 will currently fail `meshx client status`
// decode; tracked as a follow-up.
func fakeRadio(id, dest string) *radio.Session {
	sess := radio.New(nil, nil, nil)
	sess.State.RadioID = id
	sess.State.ConnectDest = dest
	sess.State.MyNodeNum = 0x12345
	sess.State.Connected = true
	return sess
}

// TestRunClientStatus — `meshx client status` prints daemon health
// + every attached radio. Single happy-path row plus an empty-radios
// row exercise the two output branches (table vs "no radios").
func TestRunClientStatus(t *testing.T) {
	cases := []struct {
		name        string
		radios      []*radio.Session
		wantSubstrs []string
	}{
		{
			name:   "happy-path-one-radio-prints-table-row",
			radios: []*radio.Session{fakeRadio("0xabcdef01", "ble:48d917af-8a1f-e43e-4735")},
			wantSubstrs: []string{
				"daemon: ok",
				"RADIO_ID",
				"0xabcdef01",
				"0x12345",
				"ble:48d917af",
				"yes", // Connected
			},
		},
		{
			name:        "empty-registry-prints-no-radios-line",
			radios:      nil,
			wantSubstrs: []string{"daemon: ok", "no radios attached"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := clientHarness(t, tc.radios...)
			var buf bytes.Buffer
			if err := runClientStatus(context.Background(), c, &buf); err != nil {
				t.Fatalf("runClientStatus: %v", err)
			}
			got := buf.String()
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
		})
	}
}

// TestResolveClientRadio — the auto-target + needle-resolution rules
// for `meshx client connect [radio]`. Each row mutates the registry
// to exercise a different lookup path.
func TestResolveClientRadio(t *testing.T) {
	type seed struct {
		id, dest string
	}
	cases := []struct {
		name    string
		seeds   []seed
		needle  string
		wantID  string
		wantErr string // substring; "" means expect no error
	}{
		{
			name:    "empty-registry-empty-needle-errors",
			seeds:   nil,
			needle:  "",
			wantErr: "no radios attached",
		},
		{
			name:   "one-radio-empty-needle-auto-targets",
			seeds:  []seed{{id: "0xabcdef01", dest: "/dev/cu.usb"}},
			needle: "",
			wantID: "0xabcdef01",
		},
		{
			name: "multiple-radios-empty-needle-errors-with-hint",
			seeds: []seed{
				{id: "0xabcdef01", dest: "/dev/cu.usb1"},
				{id: "0xabcdef02", dest: "/dev/cu.usb2"},
			},
			needle:  "",
			wantErr: "specify which",
		},
		{
			name:   "exact-radio_id-match-wins",
			seeds:  []seed{{id: "0xabcdef01", dest: "ble:foo"}},
			needle: "0xabcdef01",
			wantID: "0xabcdef01",
		},
		{
			name:   "exact-connect_dest-match-wins",
			seeds:  []seed{{id: "0xabcdef01", dest: "ble:48d917af"}},
			needle: "ble:48d917af",
			wantID: "0xabcdef01",
		},
		{
			name:   "substring-of-connect_dest-matches",
			seeds:  []seed{{id: "0xabcdef01", dest: "ble:48d917af-8a1f-..."}},
			needle: "48d917af",
			wantID: "0xabcdef01",
		},
		{
			name:    "no-match-returns-clear-error",
			seeds:   []seed{{id: "0xabcdef01", dest: "ble:foo"}},
			needle:  "nope",
			wantErr: "no radio matching",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			radios := make([]*radio.Session, 0, len(tc.seeds))
			for _, sd := range tc.seeds {
				radios = append(radios, fakeRadio(sd.id, sd.dest))
			}
			c := clientHarness(t, radios...)

			// resolveClientRadio reads ListRadios from the daemon; the
			// httptest.Server's URL isn't visible here, but the SDK
			// client already carries it, so any non-empty URL works
			// for the error-message rendering. Pull the actual URL
			// off the client by re-wiring on demand if needed — for
			// now, the resolver doesn't use serverURL for the lookup,
			// only for error messages.
			got, err := resolveClientRadio(context.Background(), c, "http://test", tc.needle)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (radio=%q)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q missing substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantID {
				t.Fatalf("radio_id = %q, want %q", got, tc.wantID)
			}
		})
	}
}
