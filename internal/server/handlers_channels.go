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
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/pump"
)

// Channel-CRUD handlers — the HTTP twin of /channel new / add / del /
// share. Raw PSK bytes never cross the API: clients either ask the
// server to mint (server generates a fresh AES256 key) or hand over a
// meshtastic:// share URL the server parses. Both paths funnel through
// SetChannel and return 202 (the radio's confirmation arrives later as
// the matching ApplyChannel event on the SSE stream).

// channelSlotsMax mirrors the firmware's hard cap on simultaneous
// channel slots — index 0 is PRIMARY, 1..7 are SECONDARY. We refuse
// to clobber slot 0 from the API (same as the TUI).
const (
	channelSlotsMax = 8
	channelNameMax  = 11
)

// MintChannelRequest creates a fresh secondary channel with a random
// AES256 PSK and a random Channel.id collision-avoidance value.
type MintChannelRequest struct {
	Name string `json:"name" doc:"channel name (1..11 bytes UTF-8); Meshtastic packs this into the share URL" minLength:"1"`
}

// MintChannelResult names the slot the new channel landed in and
// emits the meshtastic:// share URL so the caller can wrap it in
// a QR for in-person handoff. The server keeps the raw PSK bytes
// in the in-memory channels table only; they're never persisted.
type MintChannelResult struct {
	Index    int    `json:"index"     doc:"slot the channel was placed into (1..7)"`
	Name     string `json:"name"      doc:"channel name as dispatched"`
	ShareURL string `json:"share_url" doc:"meshtastic:// universal link carrying this channel's name + PSK + id"`
}

// ImportChannelRequest carries a meshtastic:// (or https://meshtastic.org/e/)
// share URL. The server parses it, then dispatches SetChannel for
// every channel inside that fits a free slot. Multi-channel URLs are
// handled per-slot — collisions are skipped, not failed.
type ImportChannelRequest struct {
	URL string `json:"url" doc:"meshtastic:// or https://meshtastic.org/e/ share URL" minLength:"1"`
}

// ImportedChannel is one entry in ImportChannelResult.Imported.
type ImportedChannel struct {
	Index int    `json:"index" doc:"slot the channel was placed into"`
	Name  string `json:"name"  doc:"channel name from the URL"`
}

// SkippedChannel is one entry in ImportChannelResult.Skipped — names
// the channel and the reason it didn't import (already exists, no
// free slot, empty name, …).
type SkippedChannel struct {
	Name   string `json:"name"   doc:"channel name from the URL"`
	Reason string `json:"reason" doc:"why this slot was skipped"`
}

// ImportChannelResult summarizes a multi-channel import. Imported is
// the slots that were actually dispatched; Skipped records names the
// server refused (collision, no free slot, empty name).
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
	name := strings.TrimSpace(in.Body.Name)
	name = strings.TrimPrefix(name, "#")
	if name == "" {
		return nil, huma.Error400BadRequest("channel name is empty")
	}
	if len(name) > channelNameMax {
		return nil, huma.Error400BadRequest(
			fmt.Sprintf("channel name %d bytes; max %d", len(name), channelNameMax),
		)
	}

	st := d.Snapshot()
	if st == nil {
		return nil, huma.Error503ServiceUnavailable("radio state unavailable")
	}
	if existingChannelIndex(st.Channels, name) >= 0 {
		return nil, huma.Error409Conflict(
			fmt.Sprintf("channel %q already exists; delete it first", name),
		)
	}
	slot := firstFreeChannelSlot(st.Channels)
	if slot < 0 {
		return nil, huma.Error409Conflict(
			fmt.Sprintf("no free channel slot (max %d secondary slots)", channelSlotsMax-1),
		)
	}

	psk := make([]byte, 32)
	if _, err := rand.Read(psk); err != nil {
		return nil, huma.Error500InternalServerError("crypto/rand: " + err.Error())
	}
	channelID, err := randUint32()
	if err != nil {
		return nil, huma.Error500InternalServerError("crypto/rand: " + err.Error())
	}

	slotInfo := mdl.ChannelInfo{
		Index:  slot,
		Name:   name,
		Role:   mdl.ChannelSecondary,
		ID:     channelID,
		HasPSK: true,
		PSK:    psk,
	}
	if _, ok := d.Send(mdl.SetChannel{Slot: slotInfo}); !ok {
		return nil, huma.Error503ServiceUnavailable(
			"radio outbound buffer full or no radio attached",
		)
	}

	shareURL, err := pump.BuildChannelShareURL(slotInfo)
	if err != nil {
		return nil, huma.Error500InternalServerError("build share url: " + err.Error())
	}

	out := &mintChannelOutput{Status: 202}
	out.Body = MintChannelResult{Index: slot, Name: name, ShareURL: shareURL}
	return out, nil
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
	url := strings.TrimSpace(in.Body.URL)
	if url == "" {
		return nil, huma.Error400BadRequest("share url is empty")
	}
	parsed, err := pump.ParseChannelShareURL(url)
	if err != nil {
		return nil, huma.Error400BadRequest("parse share url: " + err.Error())
	}
	st := d.Snapshot()
	if st == nil {
		return nil, huma.Error503ServiceUnavailable("radio state unavailable")
	}

	// Mutating a copy of the channels list keeps slot allocation
	// honest within this single import — back-to-back channels in the
	// URL each see the previously-allocated slot as taken.
	current := append([]mdl.ChannelItem{}, st.Channels...)

	out := &importChannelOutput{Status: 202}
	out.Body.Imported = []ImportedChannel{}
	out.Body.Skipped = []SkippedChannel{}

	for _, slot := range parsed {
		name := strings.TrimSpace(slot.Name)
		if name == "" {
			out.Body.Skipped = append(out.Body.Skipped, SkippedChannel{
				Reason: "empty name (would clobber default)",
			})
			continue
		}
		if existingChannelIndex(current, name) >= 0 {
			out.Body.Skipped = append(out.Body.Skipped, SkippedChannel{
				Name:   name,
				Reason: "already exists on the radio",
			})
			continue
		}
		idx := firstFreeChannelSlot(current)
		if idx < 0 {
			out.Body.Skipped = append(out.Body.Skipped, SkippedChannel{
				Name:   name,
				Reason: fmt.Sprintf("no free slot (max %d secondary)", channelSlotsMax-1),
			})
			continue
		}
		slot.Index = idx
		if slot.Role == "" || slot.Role == mdl.ChannelDisabled {
			slot.Role = mdl.ChannelSecondary
		}
		if _, ok := d.Send(mdl.SetChannel{Slot: slot}); !ok {
			out.Body.Skipped = append(out.Body.Skipped, SkippedChannel{
				Name:   name,
				Reason: "outbound buffer full",
			})
			continue
		}
		// Reflect the allocation back into the local copy so the next
		// iteration's findFree skips this slot.
		ensureChannelSlot(&current, idx)
		current[idx] = mdl.ChannelItem{
			Name:  name,
			Index: idx,
			Role:  string(slot.Role),
		}
		out.Body.Imported = append(out.Body.Imported, ImportedChannel{
			Index: idx,
			Name:  name,
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
	if in.Index <= 0 || in.Index >= channelSlotsMax {
		return nil, huma.Error400BadRequest(
			fmt.Sprintf("slot %d out of range (1..%d)", in.Index, channelSlotsMax-1),
		)
	}
	if _, ok := d.Send(mdl.DeleteChannel{Index: in.Index}); !ok {
		return nil, huma.Error503ServiceUnavailable(
			"radio outbound buffer full or no radio attached",
		)
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
	st := d.Snapshot()
	if st == nil || in.Index < 0 || in.Index >= len(st.Channels) {
		return nil, huma.Error404NotFound(fmt.Sprintf("no channel at slot %d", in.Index))
	}
	c := st.Channels[in.Index]
	if c.Role == "" || c.Role == string(mdl.ChannelDisabled) {
		return nil, huma.Error404NotFound(fmt.Sprintf("slot %d is disabled", in.Index))
	}
	url, err := pump.BuildChannelShareURL(mdl.ChannelInfo{
		Index:  c.Index,
		Name:   c.Name,
		Role:   mdl.ChannelRole(c.Role),
		ID:     0, // ChannelItem doesn't carry the firmware ID; URL recipients regenerate one if missing
		HasPSK: len(c.PSK) > 0,
		PSK:    c.PSK,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("build share url: " + err.Error())
	}
	out := &shareChannelOutput{}
	out.Body = ChannelShareResult{
		Index:    c.Index,
		Name:     c.Name,
		ShareURL: url,
	}
	return out, nil
}

// firstFreeChannelSlot returns the lowest secondary slot (1..7) that
// is DISABLED or beyond the current channels list. -1 = full. Mirrors
// the TUI's findFreeChannelSlot.
func firstFreeChannelSlot(ch []mdl.ChannelItem) int {
	for i := 1; i < channelSlotsMax; i++ {
		if i >= len(ch) {
			return i
		}
		role := ch[i].Role
		if role == "" || role == string(mdl.ChannelDisabled) {
			return i
		}
	}
	return -1
}

// existingChannelIndex looks up a channel by name (case-sensitive,
// matching how PSK names round-trip on the wire). -1 = not found or
// DISABLED.
func existingChannelIndex(ch []mdl.ChannelItem, name string) int {
	target := strings.TrimPrefix(strings.TrimSpace(name), "#")
	for i, c := range ch {
		role := c.Role
		if role == "" || role == string(mdl.ChannelDisabled) {
			continue
		}
		if strings.TrimPrefix(c.Name, "#") == target {
			return i
		}
		// Tolerate the renderer's *name* private-channel decoration.
		bare := strings.Trim(c.Name, "*")
		if bare == target {
			return i
		}
	}
	return -1
}

// ensureChannelSlot grows ch in place so ch[idx] is addressable.
// Used during import to reflect a freshly-dispatched SetChannel back
// into our local copy of State.Channels so the next iteration's
// findFree skips it.
func ensureChannelSlot(ch *[]mdl.ChannelItem, idx int) {
	for len(*ch) <= idx {
		*ch = append(*ch, mdl.ChannelItem{Index: len(*ch)})
	}
}

// randUint32 mirrors the TUI helper — crypto/rand-sourced 32-bit
// value for ChannelSettings.id collision avoidance among Meshtastic
// users.
func randUint32() (uint32, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("crypto/rand: %w", err)
	}
	return binary.BigEndian.Uint32(b[:]), nil
}
