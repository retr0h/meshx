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

package model

// CachedNode is the slim persistence shape of a peer — identity
// fields plus sticky UX preferences (favorite / muted). Returned by
// the storage layer's LoadNodes; consumed by the meshx renderer at
// startup to pre-populate its live nodeItem rows.
//
// No telemetry fields here (snr, hops, lastHeard) — those are
// per-session and the renderer derives them from live packets, not
// from the cache. The cache only carries what survives a restart.
type CachedNode struct {
	// NodeNum is the Meshtastic node num (uint32, derived from MAC).
	// Primary key for joining live NodeInfo packets back to cached
	// identity.
	NodeNum uint32

	// LongName is the user-friendly callsign as set on the radio
	// (longname can be up to 36 bytes per the User proto). Empty
	// when the radio's NodeDB only ever exposed shortname.
	LongName string

	// ShortName is the 4-byte shortname displayed alongside the
	// longname in the nodes pane. Empty for never-resolved peers.
	ShortName string

	// HwModel is the firmware-reported hardware type ("HELTEC_V3",
	// "TBEAM", etc.). Surfaced in the nodes pane for at-a-glance
	// device identification.
	HwModel string

	// Favorite reflects the user's `*` star in the nodes pane.
	// Persisted so the star survives restarts.
	Favorite bool

	// Muted reflects the user's `m` mute in the nodes pane —
	// suppresses ding + flash for this peer. Persisted so the
	// mute survives restarts.
	Muted bool
}
