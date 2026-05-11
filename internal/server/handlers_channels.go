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

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
)

// HTTP handlers for /channels — GET list + the mint / import / delete /
// share CRUD surface. Mutating methods are thin adapters over
// *radio.Session ops. Business logic (validation, PSK gen, slot
// allocation, optimistic state) lives in internal/radio/ops_channels.go
// and is shared with the TUI. The handler's only job is to map
// Huma's input/output structs to and from the session method's plain
// types.

type listChannelsInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
}

type listChannelsOutput struct {
	Body struct {
		Channels []mdl.ChannelItem `json:"channels"`
	}
}

// handleListChannels — GET /radios/{radio_id}/channels. Projects the
// snapshot's channel table without the PSK bytes (HasPSK keeps the
// has-key signal); /channels/{idx}/share is the dedicated read path
// for the PSK material (carried inside the meshtastic:// URL).
func (s *Server) handleListChannels(
	_ context.Context,
	in *listChannelsInput,
) (*listChannelsOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	out := &listChannelsOutput{}
	out.Body.Channels = []mdl.ChannelItem{}
	st := d.Snapshot()
	if st == nil {
		return out, nil
	}
	for _, c := range st.Channels {
		c.HasPSK = len(c.PSK) > 0
		c.PSK = nil
		out.Body.Channels = append(out.Body.Channels, c)
	}
	return out, nil
}

// MintChannelRequest is the inbound POST body. Wire-shape mirror of
// radio.MintChannelRequest so generated SDK clients see the right
// JSON keys.
type MintChannelRequest struct {
	Name string `json:"name" doc:"channel name (1..11 bytes UTF-8); Meshtastic packs this into the share URL" minLength:"1"`
}

// MintChannelResult is the response body. PSK is omitted on the wire
// — it's already encoded inside ShareURL for clients that need it.
type MintChannelResult struct {
	Index    int    `json:"index"     doc:"slot the channel was placed into (1..7)"`
	Name     string `json:"name"      doc:"channel name as dispatched"`
	ShareURL string `json:"share_url" doc:"meshtastic:// universal link carrying this channel's name + PSK + id"`
}

// ImportChannelRequest is the inbound POST body.
type ImportChannelRequest struct {
	URL string `json:"url" doc:"meshtastic:// or https://meshtastic.org/e/ share URL" minLength:"1"`
}

// ImportedChannel is one entry in ImportChannelResult.Imported.
type ImportedChannel struct {
	Index int    `json:"index" doc:"slot the channel was placed into"`
	Name  string `json:"name"  doc:"channel name from the URL"`
}

// SkippedChannel is one entry in ImportChannelResult.Skipped.
type SkippedChannel struct {
	Name   string `json:"name"   doc:"channel name from the URL"`
	Reason string `json:"reason" doc:"why this slot was skipped"`
}

// ImportChannelResult summarizes the import.
type ImportChannelResult struct {
	Imported []ImportedChannel `json:"imported"`
	Skipped  []SkippedChannel  `json:"skipped"`
}

// ChannelShareResult is the body of GET /channels/{idx}/share.
type ChannelShareResult struct {
	Index    int    `json:"index"     doc:"slot index 0..7"`
	Name     string `json:"name"      doc:"channel name"`
	ShareURL string `json:"share_url" doc:"meshtastic:// universal link"`
}

type mintChannelInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Body    MintChannelRequest
}

type mintChannelOutput struct {
	Status int
	Body   MintChannelResult
}

func (s *Server) handleMintChannel(
	_ context.Context,
	in *mintChannelInput,
) (*mintChannelOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	res, err := d.MintChannel(radio.MintChannelRequest{Name: in.Body.Name})
	if err != nil {
		return nil, err
	}
	return &mintChannelOutput{
		Status: 202,
		Body: MintChannelResult{
			Index:    res.Index,
			Name:     res.Name,
			ShareURL: res.ShareURL,
		},
	}, nil
}

type importChannelInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Body    ImportChannelRequest
}

type importChannelOutput struct {
	Status int
	Body   ImportChannelResult
}

func (s *Server) handleImportChannel(
	_ context.Context,
	in *importChannelInput,
) (*importChannelOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	res, err := d.ImportChannel(radio.ImportChannelRequest{URL: in.Body.URL})
	if err != nil {
		return nil, err
	}
	out := &importChannelOutput{Status: 202}
	out.Body.Imported = make([]ImportedChannel, 0, len(res.Imported))
	for _, ic := range res.Imported {
		out.Body.Imported = append(out.Body.Imported, ImportedChannel{
			Index: ic.Index,
			Name:  ic.Name,
		})
	}
	out.Body.Skipped = make([]SkippedChannel, 0, len(res.Skipped))
	for _, sc := range res.Skipped {
		out.Body.Skipped = append(out.Body.Skipped, SkippedChannel{
			Name:   sc.Name,
			Reason: sc.Reason,
		})
	}
	return out, nil
}

type deleteChannelInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Index   int    `path:"index"    doc:"slot index 1..7 (slot 0 is PRIMARY and cannot be deleted)" minimum:"1" maximum:"7"`
}

type deleteChannelOutput struct {
	Status int
}

func (s *Server) handleDeleteChannel(
	_ context.Context,
	in *deleteChannelInput,
) (*deleteChannelOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	if _, err := d.DeleteChannel(radio.DeleteChannelRequest{Index: in.Index}); err != nil {
		return nil, err
	}
	return &deleteChannelOutput{Status: 202}, nil
}

type shareChannelInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Index   int    `path:"index"    doc:"slot index 0..7"                              minimum:"0" maximum:"7"`
}

type shareChannelOutput struct {
	Body ChannelShareResult
}

func (s *Server) handleShareChannel(
	_ context.Context,
	in *shareChannelInput,
) (*shareChannelOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	res, err := d.ShareChannel(radio.ShareChannelRequest{Index: in.Index})
	if err != nil {
		return nil, err
	}
	return &shareChannelOutput{
		Body: ChannelShareResult{
			Index:    res.Index,
			Name:     res.Name,
			ShareURL: res.ShareURL,
		},
	}, nil
}
