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
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/pump"
)

// Channel ops — the single source of truth for /channel new / add /
// del / share, consumed by both the HTTP handlers in internal/server
// and the TUI's slash-command dispatcher. The methods own validation,
// PSK generation, slot allocation, command dispatch, and optimistic
// state update; consumers add their own UI feedback (TUI flash
// messages, HTTP response shaping) without duplicating logic.
//
// Errors are OpError-typed so the HTTP layer can translate to the
// appropriate status code; the message string is the human-readable
// form the TUI surfaces verbatim.

// channelSlotsMax mirrors the firmware's hard cap on simultaneous
// channel slots — index 0 is PRIMARY, 1..7 are SECONDARY. The ops
// here refuse to clobber slot 0 (firmware needs one to operate); the
// daemon's HTTP layer enforces the same with a path-level constraint.
const channelSlotsMax = 8

// channelNameMax is the byte cap Meshtastic enforces on
// ChannelSettings.Name. Names are packed into the share URL and short
// names keep the QR small and the channel selector readable.
const channelNameMax = 11

// MintChannelRequest mints a fresh secondary channel with a random
// 32-byte AES256 PSK and a random Channel.id collision-avoidance
// value.
type MintChannelRequest struct {
	// Name is the user-facing channel label. Leading "#" is trimmed
	// for convenience (TUI users type `/channel new #ham`); the bare
	// name lands on the wire.
	Name string
}

// MintChannelResult names the slot the new channel landed in and
// emits the meshtastic:// share URL so the caller can wrap it in a
// QR for in-person handoff. PSK is returned so the TUI can render a
// fingerprint; the HTTP layer redacts it (the share URL carries the
// PSK in a form clients can decode).
type MintChannelResult struct {
	Index    int    // slot 1..7 the channel was placed into
	Name     string // canonical name (after trim)
	ShareURL string // meshtastic:// universal link
	PSK      []byte // 32-byte AES256 — TUI uses for fingerprint; HTTP redacts
}

// ImportChannelRequest carries a meshtastic:// or
// https://meshtastic.org/e/ share URL. The Session parses it, then
// dispatches SetChannel for every channel inside that fits a free
// slot.
type ImportChannelRequest struct {
	URL string
}

// ImportedChannel is one entry in ImportChannelResult.Imported.
type ImportedChannel struct {
	Index int
	Name  string
}

// SkippedChannel is one entry in ImportChannelResult.Skipped —
// names the channel and the reason it didn't import.
type SkippedChannel struct {
	Name   string
	Reason string
}

// ImportChannelResult summarizes a multi-channel import. Imported is
// the slots that were actually dispatched; Skipped records names the
// session refused (collision, no free slot, empty name).
type ImportChannelResult struct {
	Imported []ImportedChannel
	Skipped  []SkippedChannel
}

// DeleteChannelRequest selects the slot to free.
type DeleteChannelRequest struct {
	Index int
}

// DeleteChannelResult returns the name of the channel that was
// freed so callers can surface "deleted X" feedback without
// re-looking-up the slot.
type DeleteChannelResult struct {
	Name string
}

// ShareChannelRequest selects the slot whose share URL to build.
type ShareChannelRequest struct {
	Index int
}

// ChannelShareResult is the body of GET /channels/{idx}/share — the
// meshtastic:// URL the caller wraps in a QR.
type ChannelShareResult struct {
	Index    int
	Name     string
	ShareURL string
}

// MintChannel — generates a 32-byte AES256 PSK, allocates the
// lowest-free secondary slot, dispatches SetChannel, optimistically
// updates State.Channels so a follow-up Mint doesn't race, builds
// the share URL.
func (s *Session) MintChannel(req MintChannelRequest) (MintChannelResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(req.Name)
	name = strings.TrimPrefix(name, "#")
	if name == "" {
		return MintChannelResult{}, ErrBadRequest("channel name is empty")
	}
	if len(name) > channelNameMax {
		return MintChannelResult{}, ErrBadRequestf(
			"channel name %d bytes; max %d",
			len(name),
			channelNameMax,
		)
	}
	if s.State == nil {
		return MintChannelResult{}, ErrUnavailable("radio state unavailable")
	}
	if existingChannelIndex(s.State.Channels, name) >= 0 {
		return MintChannelResult{}, ErrConflictf("channel %q already exists; delete it first", name)
	}
	slot := firstFreeChannelSlot(s.State.Channels)
	if slot < 0 {
		return MintChannelResult{}, ErrConflictf(
			"no free channel slot (max %d secondary slots)",
			channelSlotsMax-1,
		)
	}

	psk := make([]byte, 32)
	if _, err := rand.Read(psk); err != nil {
		return MintChannelResult{}, ErrInternal("crypto/rand: " + err.Error())
	}
	channelID, err := randUint32()
	if err != nil {
		return MintChannelResult{}, ErrInternal("crypto/rand: " + err.Error())
	}

	slotInfo := mdl.ChannelInfo{
		Index:  slot,
		Name:   name,
		Role:   mdl.ChannelSecondary,
		ID:     channelID,
		HasPSK: true,
		PSK:    psk,
	}
	if _, ok := s.Send(mdl.SetChannel{Slot: slotInfo}); !ok {
		return MintChannelResult{}, ErrUnavailable(
			"radio outbound buffer full or no radio attached",
		)
	}

	// Optimistic: reserve the slot in local state so a back-to-back
	// MintChannel can't race the radio's ChannelInfo broadcast and
	// pick the same one.
	ensureChannelSlot(&s.State.Channels, slot)
	s.State.Channels[slot] = mdl.ChannelItem{
		Name:    name,
		Private: true,
		Index:   slot,
		Role:    string(mdl.ChannelSecondary),
		HasPSK:  true,
		PSK:     psk,
	}

	shareURL, err := pump.BuildChannelShareURL(slotInfo)
	if err != nil {
		return MintChannelResult{}, ErrInternal("build share url: " + err.Error())
	}
	return MintChannelResult{
		Index:    slot,
		Name:     name,
		ShareURL: shareURL,
		PSK:      psk,
	}, nil
}

// ImportChannel — parses a meshtastic:// share URL and dispatches
// SetChannel for every channel inside that fits a free slot. Multi-
// channel URLs are handled per-slot — collisions and full-slot-table
// conditions are recorded in Skipped[] rather than failing the whole
// call (matches the TUI's "additive only" semantics).
func (s *Session) ImportChannel(req ImportChannelRequest) (ImportChannelResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return ImportChannelResult{}, ErrBadRequest("share url is empty")
	}
	parsed, err := pump.ParseChannelShareURL(url)
	if err != nil {
		return ImportChannelResult{}, ErrBadRequest("parse share url: " + err.Error())
	}
	if s.State == nil {
		return ImportChannelResult{}, ErrUnavailable("radio state unavailable")
	}

	// Mutate a copy of State.Channels so slot allocation stays honest
	// within this single import — back-to-back channels in the URL
	// each see the previously-allocated slot as taken.
	current := append([]mdl.ChannelItem{}, s.State.Channels...)

	out := ImportChannelResult{
		Imported: []ImportedChannel{},
		Skipped:  []SkippedChannel{},
	}
	for _, slot := range parsed {
		name := strings.TrimSpace(slot.Name)
		if name == "" {
			out.Skipped = append(out.Skipped, SkippedChannel{
				Reason: "empty name (would clobber default)",
			})
			continue
		}
		if existingChannelIndex(current, name) >= 0 {
			out.Skipped = append(out.Skipped, SkippedChannel{
				Name:   name,
				Reason: "already exists on the radio",
			})
			continue
		}
		idx := firstFreeChannelSlot(current)
		if idx < 0 {
			out.Skipped = append(out.Skipped, SkippedChannel{
				Name:   name,
				Reason: fmt.Sprintf("no free slot (max %d secondary)", channelSlotsMax-1),
			})
			continue
		}
		slot.Index = idx
		if slot.Role == "" || slot.Role == mdl.ChannelDisabled {
			slot.Role = mdl.ChannelSecondary
		}
		if _, ok := s.Send(mdl.SetChannel{Slot: slot}); !ok {
			out.Skipped = append(out.Skipped, SkippedChannel{
				Name:   name,
				Reason: "outbound buffer full",
			})
			continue
		}
		// Reflect into both the working copy (so next-iteration
		// findFree skips this slot) and State.Channels (so consumers
		// see it before the radio confirms via ChannelInfo).
		ensureChannelSlot(&current, idx)
		current[idx] = mdl.ChannelItem{
			Name:    name,
			Private: slot.HasPSK,
			Index:   idx,
			Role:    string(slot.Role),
			HasPSK:  slot.HasPSK,
			PSK:     slot.PSK,
		}
		ensureChannelSlot(&s.State.Channels, idx)
		s.State.Channels[idx] = current[idx]
		out.Imported = append(out.Imported, ImportedChannel{Index: idx, Name: name})
	}
	return out, nil
}

// DeleteChannel — disables a slot. Refuses slot 0 (PRIMARY) since
// the firmware requires one to operate. Optimistically clears local
// state.
func (s *Session) DeleteChannel(req DeleteChannelRequest) (DeleteChannelResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Index <= 0 || req.Index >= channelSlotsMax {
		return DeleteChannelResult{}, ErrBadRequestf(
			"slot %d out of range (1..%d)",
			req.Index,
			channelSlotsMax-1,
		)
	}
	if s.State == nil {
		return DeleteChannelResult{}, ErrUnavailable("radio state unavailable")
	}
	var deletedName string
	if req.Index < len(s.State.Channels) {
		deletedName = s.State.Channels[req.Index].Name
	}
	if _, ok := s.Send(mdl.DeleteChannel{Index: req.Index}); !ok {
		return DeleteChannelResult{}, ErrUnavailable(
			"radio outbound buffer full or no radio attached",
		)
	}
	if req.Index < len(s.State.Channels) {
		s.State.Channels[req.Index] = mdl.ChannelItem{
			Index: req.Index,
			Role:  string(mdl.ChannelDisabled),
		}
	}
	return DeleteChannelResult{Name: deletedName}, nil
}

// ShareChannel — builds a meshtastic:// URL for the named slot. 404
// for slot indexes beyond the channel table or for DISABLED slots.
func (s *Session) ShareChannel(req ShareChannelRequest) (ChannelShareResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == nil ||
		req.Index < 0 || req.Index >= len(s.State.Channels) {
		return ChannelShareResult{}, ErrNotFoundf("no channel at slot %d", req.Index)
	}
	c := s.State.Channels[req.Index]
	if c.Role == "" || c.Role == string(mdl.ChannelDisabled) {
		return ChannelShareResult{}, ErrNotFoundf("slot %d is disabled", req.Index)
	}
	url, err := pump.BuildChannelShareURL(mdl.ChannelInfo{
		Index:  c.Index,
		Name:   c.Name,
		Role:   mdl.ChannelRole(c.Role),
		ID:     0, // ChannelItem doesn't carry the firmware ID
		HasPSK: len(c.PSK) > 0,
		PSK:    c.PSK,
	})
	if err != nil {
		return ChannelShareResult{}, ErrInternal("build share url: " + err.Error())
	}
	return ChannelShareResult{
		Index:    c.Index,
		Name:     c.Name,
		ShareURL: url,
	}, nil
}

// LookupChannelByName resolves a user-typed channel name to its slot
// index. Accepts the bare name, "#name", or "*name*" (renderer
// display forms). Returns -1 when no live channel matches — callers
// surface a "no channel matching X" message.
//
// Lives here so the TUI's findChannelByName + the daemon's
// future name-based admin commands share one resolution rule.
func (s *Session) LookupChannelByName(typed string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == nil {
		return -1
	}
	want := bareChannelName(strings.TrimSpace(typed))
	for i, c := range s.State.Channels {
		if c.Role == "" || c.Role == string(mdl.ChannelDisabled) {
			continue
		}
		if bareChannelName(c.Name) == want {
			return i
		}
	}
	return -1
}

// bareChannelName strips the renderer's display prefixes from a
// channel name — "#" for unkeyed channels, "*…*" for PSK-protected.
// Used by LookupChannelByName so the user-typed name and the
// radio-side ChannelSettings.Name stay in sync.
func bareChannelName(s string) string {
	s = strings.TrimPrefix(s, "#")
	return strings.Trim(s, "*")
}

// firstFreeChannelSlot returns the lowest secondary slot (1..7) that
// is DISABLED or beyond the current channels list. -1 = full.
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
	target := bareChannelName(strings.TrimSpace(name))
	for i, c := range ch {
		role := c.Role
		if role == "" || role == string(mdl.ChannelDisabled) {
			continue
		}
		if bareChannelName(c.Name) == target {
			return i
		}
	}
	return -1
}

// ensureChannelSlot grows ch in place so ch[idx] is addressable.
// Used during import + mint to reflect a freshly-dispatched
// SetChannel back into local state so the next iteration's findFree
// skips it.
func ensureChannelSlot(ch *[]mdl.ChannelItem, idx int) {
	for len(*ch) <= idx {
		*ch = append(*ch, mdl.ChannelItem{Index: len(*ch)})
	}
}

// randUint32 — crypto/rand-sourced 32-bit value for
// ChannelSettings.id collision avoidance among Meshtastic users.
func randUint32() (uint32, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("crypto/rand: %w", err)
	}
	return binary.BigEndian.Uint32(b[:]), nil
}
