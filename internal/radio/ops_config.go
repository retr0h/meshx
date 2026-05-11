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

package radio

import (
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Config-mutation ops — single source of truth for /nick, /tag,
// /config buzzer, /reboot. HTTP handlers (handlers_config.go) and
// TUI slash commands both call into these methods. Validation +
// dispatch happens once; consumers add UI feedback / wire shaping.
//
// Errors are huma-typed so HTTP gets correct status codes (400 for
// validation, 503 for buffer full / no radio) without translation.

// ownerByteLongMax / ownerByteShortMax mirror the firmware's User
// record byte limits. UTF-8-byte-counted, not rune-counted, because
// that's what the proto field carries on the wire.
const (
	ownerByteLongMax  = 36
	ownerByteShortMax = 4
)

// defaultRebootSeconds is the grace window for /reboot when no
// explicit Seconds is supplied. Matches the TUI's historical default
// and gives the operator a moment to abort.
const defaultRebootSeconds = 5

// UpdateConfigRequest is a sparse update — every field is a pointer
// so "absent" and "explicit zero/empty" are distinguishable. Only
// fields the caller supplies are dispatched.
type UpdateConfigRequest struct {
	LongName   *string
	ShortName  *string
	IsLicensed *bool
	Buzzer     *bool
}

// UpdateConfigResult names the fields that actually got dispatched
// to the radio — subset of the request body. Empty Applied means
// the request was a no-op (every field already matched current
// state).
type UpdateConfigResult struct {
	Applied []string
}

// RebootRequest schedules a firmware reboot. Seconds == 0 falls
// back to the default grace window; explicit non-zero schedules N
// seconds out.
type RebootRequest struct {
	Seconds int32
}

// RebootResult echoes the delay the radio acknowledged.
type RebootResult struct {
	Seconds int32
}

// UpdateConfig — partial PATCH for the radio's User record (longname
// / shortname / is_licensed) and ExternalNotification buzzer. Owner
// fields coalesce into one SetOwner dispatch since the firmware
// overwrites the whole User record atomically; the buzzer is a
// separate SetBuzzer (different AdminMessage envelope).
//
// Validation runs BEFORE Send so a bad longname rejects with 400
// instead of dispatching a doomed AdminMessage and watching the
// radio silently drop it.
func (s *Session) UpdateConfig(req UpdateConfigRequest) (UpdateConfigResult, error) {
	if req.LongName != nil {
		if n := len(*req.LongName); n == 0 || n > ownerByteLongMax {
			return UpdateConfigResult{}, huma.Error400BadRequest(
				fmt.Sprintf("longname %d bytes; must be 1..%d", n, ownerByteLongMax),
			)
		}
	}
	if req.ShortName != nil {
		if n := len(*req.ShortName); n == 0 || n > ownerByteShortMax {
			return UpdateConfigResult{}, huma.Error400BadRequest(
				fmt.Sprintf("shortname %d bytes; must be 1..%d", n, ownerByteShortMax),
			)
		}
	}

	out := UpdateConfigResult{Applied: []string{}}

	// Owner — longname / shortname / is_licensed share one
	// AdminMessage envelope; coalesce into one SetOwner and read
	// missing fields off current State so a partial PATCH preserves
	// the omitted bits.
	if req.LongName != nil || req.ShortName != nil || req.IsLicensed != nil {
		long, short, licensed := s.currentOwner()
		if req.LongName != nil {
			long = *req.LongName
			out.Applied = append(out.Applied, "longname")
		}
		if req.ShortName != nil {
			short = *req.ShortName
			out.Applied = append(out.Applied, "shortname")
		}
		if req.IsLicensed != nil {
			licensed = *req.IsLicensed
			out.Applied = append(out.Applied, "is_licensed")
		}
		if _, ok := s.Send(mdl.SetOwner{
			LongName:   long,
			ShortName:  short,
			IsLicensed: licensed,
		}); !ok {
			return UpdateConfigResult{}, huma.Error503ServiceUnavailable(
				"radio outbound buffer full or no radio attached",
			)
		}
	}

	if req.Buzzer != nil {
		var snap mdl.ExternalNotification
		if s.State != nil {
			snap = s.State.RadioBuzzerSnapshot
		}
		if _, ok := s.Send(mdl.SetBuzzer{
			Enabled:  *req.Buzzer,
			Snapshot: snap,
		}); !ok {
			return UpdateConfigResult{}, huma.Error503ServiceUnavailable(
				"radio outbound buffer full or no radio attached",
			)
		}
		out.Applied = append(out.Applied, "buzzer")
	}

	return out, nil
}

// currentOwner pulls the radio's own current longname / shortname /
// is_licensed off State so a partial UpdateConfig preserves omitted
// fields. Returns zero values when state isn't populated yet — the
// byte-cap validator above catches "empty longname" before it can
// hit Send.
//
// IsLicensed isn't currently surfaced on NodeItem (the firmware's
// User record carries it but the model doesn't replay it through);
// leave it false so a bare-bones patch doesn't accidentally clear
// the licensed flag — clients that want to keep it set must pass
// is_licensed=true explicitly.
func (s *Session) currentOwner() (string, string, bool) {
	if s == nil || s.State == nil || s.State.MyNodeNum == 0 {
		return "", "", false
	}
	idx, ok := s.State.NodesByNum[s.State.MyNodeNum]
	if !ok || idx >= len(s.State.Nodes) {
		return "", "", false
	}
	n := s.State.Nodes[idx]
	return n.Callsign, n.ShortName, false
}

// Reboot — schedules a firmware reboot. Seconds <= 0 defaults to the
// 5s grace window. The radio drops carrier on reboot; the pump's
// reconnect loop reattaches when it comes back, so callers don't
// have to do anything special after dispatch.
func (s *Session) Reboot(req RebootRequest) (RebootResult, error) {
	secs := req.Seconds
	if secs <= 0 {
		secs = defaultRebootSeconds
	}
	if _, ok := s.Send(mdl.Reboot{Seconds: secs}); !ok {
		return RebootResult{}, huma.Error503ServiceUnavailable(
			"radio outbound buffer full or no radio attached",
		)
	}
	return RebootResult{Seconds: secs}, nil
}
