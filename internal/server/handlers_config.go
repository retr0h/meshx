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

	"github.com/retr0h/meshx/internal/radio"
)

// HTTP handlers for PATCH /config + POST /reboot — thin adapters
// over *radio.Session ops. Validation, owner-field coalescing, and
// dispatch live in internal/radio/ops_config.go and are shared with
// the TUI's /nick / /tag / /config / /reboot commands.

// UpdateConfigRequest is the sparse PATCH body. Pointer fields so
// "absent" and "explicit empty" are distinguishable.
type UpdateConfigRequest struct {
	Buzzer     *bool   `json:"buzzer,omitempty"      doc:"toggle the radio's external-notification buzzer (true = on, false = off)"`
	LongName   *string `json:"longname,omitempty"    doc:"radio operator longname (1..36 bytes UTF-8)"`
	ShortName  *string `json:"shortname,omitempty"   doc:"radio operator shortname (1..4 bytes UTF-8 — emoji counts as its byte length)"`
	IsLicensed *bool   `json:"is_licensed,omitempty" doc:"FCC-licensed flag on the User record; preserved when omitted"`
}

// UpdateConfigResult names the fields the radio acknowledged.
type UpdateConfigResult struct {
	Applied []string `json:"applied" doc:"names of config fields dispatched to the radio (subset of the request body)"`
}

// RebootRequest schedules a firmware reboot.
type RebootRequest struct {
	Seconds int32 `json:"seconds,omitempty" doc:"delay before reboot in seconds; 0 = now (default 5 matches the TUI's /reboot grace)" maximum:"3600" minimum:"0"`
}

// RebootResult acknowledges the dispatch.
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
	res, err := d.UpdateConfig(radio.UpdateConfigRequest{
		LongName:   in.Body.LongName,
		ShortName:  in.Body.ShortName,
		IsLicensed: in.Body.IsLicensed,
		Buzzer:     in.Body.Buzzer,
	})
	if err != nil {
		return nil, err
	}
	out := &updateConfigOutput{Status: 202}
	out.Body = UpdateConfigResult{Applied: res.Applied}
	return out, nil
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
	res, err := d.Reboot(radio.RebootRequest{Seconds: in.Body.Seconds})
	if err != nil {
		return nil, err
	}
	return &rebootOutput{Status: 202, Body: RebootResult{Seconds: res.Seconds}}, nil
}
