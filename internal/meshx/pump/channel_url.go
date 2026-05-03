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

package pump

// channel_url.go — encode + decode for the meshtastic:// share-link
// format. The link is just a base64-url protobuf — no network round-
// trip is involved. meshtastic.org's `/e/` page is a fallback landing
// surface that decodes the URL fragment client-side; the PSK never
// touches a server.
//
// Two URL shapes both ship the same payload:
//
//   meshtastic://e/#<base64url ChannelSet>
//   https://meshtastic.org/e/#<base64url ChannelSet>[?add=true]
//
// The `?add=true` query is the official phone app's "add to existing
// channels" hint; consumers always treat imports as additive (never
// wipe the radio's other channels) so the flag is informational.
//
// Lives in pump because it touches gomeshproto — meshx never sees
// pb.ChannelSet / pb.ChannelSettings on either side. The functions
// take + return model.* types; the proto encode/decode is internal.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// channelShareURLPrefix is the "universal link" form of the share
// URL. /channel share emits this because it's the form that works
// for recipients regardless of whether they have a Meshtastic app
// installed — with the app, the OS deep-links into it; without, the
// browser falls back to meshtastic.org's landing page.
const channelShareURLPrefix = "https://meshtastic.org/e/#"

// ParseChannelShareURL accepts either a `meshtastic://e/#…` deep
// link or an `https://meshtastic.org/e/#…` universal link, decodes
// the base64-url protobuf payload, and returns the contained
// channels as flat model values. Used by /channel add — meshx never
// sees pb.ChannelSet.
//
// LoraConfig from the share URL is dropped today (matches the
// previous behavior — no consumer applies it). When that lands,
// extend the return signature to surface model.LoraConfig.
func ParseChannelShareURL(raw string) ([]model.ChannelInfo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	switch {
	case u.Scheme == "meshtastic" && u.Host == "e":
		// meshtastic://e/#... — fragment carries the payload.
	case (u.Scheme == "https" || u.Scheme == "http") &&
		u.Host == "meshtastic.org" &&
		strings.HasPrefix(u.Path, "/e/"):
		// meshtastic.org/e/#... — same.
	default:
		return nil, errors.New(
			"unrecognized url scheme — expected meshtastic://e/ or https://meshtastic.org/e/",
		)
	}
	frag := u.Fragment
	if frag == "" {
		return nil, errors.New("url has no payload after #")
	}
	// Strip optional `?add=true` (or any trailing query) — the
	// channel set is everything up to the first `?`.
	if i := strings.IndexByte(frag, '?'); i >= 0 {
		frag = frag[:i]
	}
	// Meshtastic uses URL-safe base64 *without padding*, matching
	// the canonical Python `urlsafe_b64encode().rstrip("=")`. Be
	// lenient if a sender included padding by stripping `=` then
	// running the unpadded decoder once.
	frag = strings.TrimRight(frag, "=")
	payload, err := base64.RawURLEncoding.DecodeString(frag)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	cs := &pb.ChannelSet{}
	if err := proto.Unmarshal(payload, cs); err != nil {
		return nil, fmt.Errorf("unmarshal ChannelSet: %w", err)
	}
	settings := cs.GetSettings()
	if len(settings) == 0 {
		return nil, errors.New("channel set has no channels")
	}
	out := make([]model.ChannelInfo, 0, len(settings))
	for _, s := range settings {
		// Defensive copy: the proto's Psk slice aliases the decoded
		// payload buffer, which the consumer might pin past the
		// lifetime of cs. Cheap to copy 16-32 bytes.
		var pskCopy []byte
		if psk := s.GetPsk(); len(psk) > 0 {
			pskCopy = append([]byte(nil), psk...)
		}
		out = append(out, model.ChannelInfo{
			Name:   s.GetName(),
			Role:   model.ChannelSecondary, // share URLs always carry secondaries
			ID:     s.GetId(),
			HasPSK: len(pskCopy) > 0,
			PSK:    pskCopy,
		})
	}
	return out, nil
}

// BuildChannelShareURL is the inverse — wraps a single channel slot
// in a ChannelSet (the URL format always carries a set, even of
// one) and emits the universal `https://meshtastic.org/e/#…` link.
//
// LoRa config from the live radio could ride along so the recipient
// matches region / modem preset — the official phone app does that.
// Today we leave it nil (PSK + name is enough to join a single
// channel) and leave room to thread it through later via a second
// argument.
func BuildChannelShareURL(slot model.ChannelInfo) (string, error) {
	cs := &pb.ChannelSet{
		Settings: []*pb.ChannelSettings{{
			Name: slot.Name,
			Psk:  slot.PSK,
			Id:   slot.ID,
		}},
	}
	raw, err := proto.Marshal(cs)
	if err != nil {
		return "", fmt.Errorf("marshal ChannelSet: %w", err)
	}
	return channelShareURLPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
