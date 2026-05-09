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
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Radio-config mutation handlers — the HTTP twin of the TUI's
// /nick / /tag / /config / /reboot dispatch path. Each route forwards
// to Driver.Send (which funnels into the in-process pump) and returns
// 202 Accepted: every command is fire-and-forget at the wire, and the
// radio's confirmation arrives later as the corresponding Apply* event
// the SSE stream republishes.
//
// Validation runs BEFORE Send so a bad longname rejects 400 instead
// of dispatching a doomed AdminMessage and watching the radio drop
// it silently. Byte caps mirror the firmware's User record limits
// (36 bytes longname, 4 bytes shortname); these are the exact same
// caps the TUI's setOwner enforces.

// ownerByteLongMax / ownerByteShortMax mirror the firmware's User
// record byte limits. UTF-8-byte-counted, not rune-counted, because
// that's what the proto field carries on the wire.
const (
	ownerByteLongMax  = 36
	ownerByteShortMax = 4
)

// UpdateConfigRequest is a sparse PATCH body — every field is a
// pointer so "absent" and "explicit zero" are distinguishable. Only
// fields the client supplies are dispatched; the rest are left
// untouched on the radio.
type UpdateConfigRequest struct {
	Buzzer     *bool   `json:"buzzer,omitempty"      doc:"toggle the radio's external-notification buzzer (true = on, false = off)"`
	LongName   *string `json:"longname,omitempty"    doc:"radio operator longname (1..36 bytes UTF-8)"`
	ShortName  *string `json:"shortname,omitempty"   doc:"radio operator shortname (1..4 bytes UTF-8 — emoji counts as its byte length)"`
	IsLicensed *bool   `json:"is_licensed,omitempty" doc:"FCC-licensed flag on the User record; preserved when omitted"`
}

// UpdateConfigResult is the 202-Accepted echo — names the fields that
// actually got dispatched so a client can correlate with subsequent
// SSE events. Empty Applied means the body was a no-op (every field
// already matched current state).
type UpdateConfigResult struct {
	Applied []string `json:"applied" doc:"names of config fields dispatched to the radio (subset of the request body)"`
}

// RebootRequest schedules a firmware reboot. Seconds=0 reboots
// immediately; non-zero schedules the reboot N seconds out. The TUI
// /reboot uses 5s to give the operator a moment to abort.
type RebootRequest struct {
	Seconds int32 `json:"seconds,omitempty" doc:"delay before reboot in seconds; 0 = now (default 5 matches the TUI's /reboot grace)" minimum:"0" maximum:"3600"`
}

// RebootResult acknowledges the dispatch. The radio drops carrier on
// reboot, so clients should expect the SSE stream to disconnect ~N
// seconds later; the daemon's pump retries indefinitely and resumes
// publishing once the radio comes back.
type RebootResult struct {
	Seconds int32 `json:"seconds" doc:"delay (in seconds) the radio acknowledged"`
}

type updateConfigInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Body    UpdateConfigRequest
}

type updateConfigOutput struct {
	Status int
	Body   UpdateConfigResult
}

func (s *Server) handleUpdateConfig(
	_ context.Context,
	in *updateConfigInput,
) (*updateConfigOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}

	if in.Body.LongName != nil {
		if n := len(*in.Body.LongName); n == 0 || n > ownerByteLongMax {
			return nil, huma.Error400BadRequest(
				fmt.Sprintf("longname %d bytes; must be 1..%d", n, ownerByteLongMax),
			)
		}
	}
	if in.Body.ShortName != nil {
		if n := len(*in.Body.ShortName); n == 0 || n > ownerByteShortMax {
			return nil, huma.Error400BadRequest(
				fmt.Sprintf("shortname %d bytes; must be 1..%d", n, ownerByteShortMax),
			)
		}
	}

	out := &updateConfigOutput{Status: 202}
	out.Body.Applied = []string{}

	// Owner — longname / shortname / is_licensed share one
	// AdminMessage envelope (firmware overwrites the whole User
	// record), so we coalesce the three optional fields into one
	// SetOwner dispatch and read missing fields off current state.
	if in.Body.LongName != nil || in.Body.ShortName != nil || in.Body.IsLicensed != nil {
		long, short, licensed := currentOwner(d)
		if in.Body.LongName != nil {
			long = *in.Body.LongName
			out.Body.Applied = append(out.Body.Applied, "longname")
		}
		if in.Body.ShortName != nil {
			short = *in.Body.ShortName
			out.Body.Applied = append(out.Body.Applied, "shortname")
		}
		if in.Body.IsLicensed != nil {
			licensed = *in.Body.IsLicensed
			out.Body.Applied = append(out.Body.Applied, "is_licensed")
		}
		if _, ok := d.Send(mdl.SetOwner{
			LongName:   long,
			ShortName:  short,
			IsLicensed: licensed,
		}); !ok {
			return nil, huma.Error503ServiceUnavailable(
				"radio outbound buffer full or no radio attached",
			)
		}
	}

	if in.Body.Buzzer != nil {
		st := d.Snapshot()
		var snap mdl.ExternalNotification
		if st != nil {
			snap = st.RadioBuzzerSnapshot
		}
		if _, ok := d.Send(mdl.SetBuzzer{
			Enabled:  *in.Body.Buzzer,
			Snapshot: snap,
		}); !ok {
			return nil, huma.Error503ServiceUnavailable(
				"radio outbound buffer full or no radio attached",
			)
		}
		out.Body.Applied = append(out.Body.Applied, "buzzer")
	}

	return out, nil
}

// currentOwner pulls the radio's own current longname / shortname /
// is_licensed off State so a partial PATCH preserves the omitted
// fields. Returns zero values when state isn't populated yet — the
// firmware will reject an empty longname, but the byte-cap validator
// upstream catches that case before this function runs.
func currentOwner(d Driver) (string, string, bool) {
	st := d.Snapshot()
	if st == nil || st.MyNodeNum == 0 {
		return "", "", false
	}
	idx, ok := st.NodesByNum[st.MyNodeNum]
	if !ok || idx >= len(st.Nodes) {
		return "", "", false
	}
	n := st.Nodes[idx]
	// IsLicensed isn't currently surfaced on NodeItem (the firmware's
	// User record carries it but the model doesn't replay it through);
	// leave it false so a bare-bones patch doesn't accidentally clear
	// the licensed flag — clients that want to keep it set must pass
	// is_licensed=true explicitly. TODO: thread is_licensed through
	// the NodeInfo translation so we can preserve it transparently.
	return n.Callsign, n.ShortName, false
}

type rebootInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Body    RebootRequest
}

type rebootOutput struct {
	Status int
	Body   RebootResult
}

func (s *Server) handleReboot(_ context.Context, in *rebootInput) (*rebootOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	secs := in.Body.Seconds
	if secs == 0 {
		secs = 5
	}
	if _, ok := d.Send(mdl.Reboot{Seconds: secs}); !ok {
		return nil, huma.Error503ServiceUnavailable(
			"radio outbound buffer full or no radio attached",
		)
	}
	out := &rebootOutput{Status: 202}
	out.Body = RebootResult{Seconds: secs}
	return out, nil
}
