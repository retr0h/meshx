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

package meshx

// channel_url.go — encode + decode for the meshtastic:// share link
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
// channels" hint; we always treat imports as additive (never wipe the
// radio's other channels) so the flag is informational only.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"
)

// channelShareURLPrefix is the "universal link" form of the share URL.
// We emit this from /channel share because it's the form that works
// for recipients regardless of whether they have a Meshtastic app
// installed — with the app, the OS deep-links into it; without, the
// browser falls back to meshtastic.org's landing page.
const channelShareURLPrefix = "https://meshtastic.org/e/#"

// parseChannelShareURL accepts either a `meshtastic://e/#…` deep link
// or an `https://meshtastic.org/e/#…` universal link, extracts the
// base64-url payload, and decodes it into a ChannelSet. The fragment
// after `#` is the protobuf — anything before is just routing.
func parseChannelShareURL(raw string) (*pb.ChannelSet, error) {
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
		// url.Parse moves anything after `#` into Fragment, but some
		// terminals chop the fragment when the user paste-deletes the
		// `?` query separator. Try the path tail too.
		if i := strings.LastIndex(raw, "#"); i >= 0 {
			frag = raw[i+1:]
		}
	}
	if frag == "" {
		return nil, errors.New("url has no payload after #")
	}
	// Strip optional `?add=true` (or any trailing query) — the channel
	// set is everything up to the first `?`.
	if i := strings.IndexByte(frag, '?'); i >= 0 {
		frag = frag[:i]
	}
	// Meshtastic uses URL-safe base64 *without padding*, matching the
	// canonical Python implementation's `urlsafe_b64encode().rstrip("=")`.
	bytes, err := base64.RawURLEncoding.DecodeString(frag)
	if err != nil {
		// Be lenient: some senders include padding; try the padded
		// decoder before giving up.
		bytes2, err2 := base64.URLEncoding.DecodeString(frag)
		if err2 != nil {
			return nil, fmt.Errorf("base64 decode: %w", err)
		}
		bytes = bytes2
	}
	cs := &pb.ChannelSet{}
	if err := proto.Unmarshal(bytes, cs); err != nil {
		return nil, fmt.Errorf("unmarshal ChannelSet: %w", err)
	}
	if len(cs.GetSettings()) == 0 {
		return nil, errors.New("channel set has no channels")
	}
	return cs, nil
}

// buildChannelShareURL is the inverse — wraps a single ChannelSettings
// in a ChannelSet (the URL format always carries a set, even of one)
// and emits the universal `https://meshtastic.org/e/#…` link.
//
// loraConfig is optional. The phone app's share-channel flow includes
// the radio's current LoRa config so the recipient can match region /
// modem preset; we leave it nil for now (PSK + name is enough to join
// a single channel) and leave room to wire it through later.
func buildChannelShareURL(s *pb.ChannelSettings, loraConfig *pb.Config_LoRaConfig) (string, error) {
	cs := &pb.ChannelSet{
		Settings:   []*pb.ChannelSettings{s},
		LoraConfig: loraConfig,
	}
	bytes, err := proto.Marshal(cs)
	if err != nil {
		return "", fmt.Errorf("marshal ChannelSet: %w", err)
	}
	return channelShareURLPrefix + base64.RawURLEncoding.EncodeToString(bytes), nil
}
