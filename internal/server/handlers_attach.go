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

	"github.com/danielgtaylor/huma/v2"
)

// POST /radios/attach — dial a radio at runtime without restarting
// the daemon. DELETE /radios/{radio_id} — tear down pump + remove.
// The actual dial/pump wiring is injected via Config.Attacher so
// internal/server doesn't import the concrete pump package.

// RadioAttacher is the consumer-side interface for hot-attach /
// hot-detach of radios at runtime. Declared here per the osapi-io
// pattern; the concrete implementation lives in cmd/server_start.go
// where the pump, sink, and store are wired.
type RadioAttacher interface {
	AttachRadio(ctx context.Context, dest string) (radioID string, err error)
	DetachRadio(ctx context.Context, radioID string) error
}

// AttachRadioRequest is the POST body.
type AttachRadioRequest struct {
	Dest string `json:"dest" doc:"transport target: /dev/cu.usb…, ble:<uuid>, host:port" minLength:"1"`
}

// AttachRadioResult echoes the registered radio_id (pending:<dest>
// until MyInfo arrives and the registry rekeys to 0x<nodenum>).
type AttachRadioResult struct {
	RadioID string `json:"radio_id" doc:"registered radio identifier — pending:<dest> until the handshake completes, then 0x<hex node_num>"`
}

type attachRadioInput struct {
	Body AttachRadioRequest
}

type attachRadioOutput struct {
	Status int
	Body   AttachRadioResult
}

func (s *Server) handleAttachRadio(
	_ context.Context,
	in *attachRadioInput,
) (*attachRadioOutput, error) {
	if s.attacher == nil {
		return nil, huma.Error503ServiceUnavailable(
			"radio attach not available (no attacher configured)",
		)
	}
	radioID, err := s.attacher.AttachRadio(context.Background(), in.Body.Dest)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &attachRadioOutput{
		Status: 202,
		Body:   AttachRadioResult{RadioID: radioID},
	}, nil
}

type detachRadioInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
}

type detachRadioOutput struct {
	Status int
}

func (s *Server) handleDetachRadio(
	_ context.Context,
	in *detachRadioInput,
) (*detachRadioOutput, error) {
	if s.attacher == nil {
		return nil, huma.Error503ServiceUnavailable(
			"radio detach not available (no attacher configured)",
		)
	}
	if err := s.attacher.DetachRadio(context.Background(), in.RadioID); err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	return &detachRadioOutput{Status: 200}, nil
}
