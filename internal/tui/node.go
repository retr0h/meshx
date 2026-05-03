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

package tui

// node.go — peer identity, derived state, and lookup.
//
// Groups the read-side node helpers the rest of the package uses
// to answer "who is this callsign" / "am I online" / "who is my
// own radio" questions. Radio-packet handlers that MUTATE node
// state (upsertNode, applyTextMessage) live in radio.go; purely-
// derived display state (CurrentState, nodeLastHeard) live
// here as a free function so every call-site reads the same
// computed values instead of poking a stale string field.

import (
	"fmt"
	"strings"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// nodeState is a type alias to mdl.NodeState so existing TUI code
// that references the local name continues to compile without change.
type nodeState = mdl.NodeState

// Local aliases for the mdl.NodeState constants so existing TUI code
// that references stateOnline / stateOffline etc. compiles unchanged.
const (
	stateUnknown nodeState = mdl.StateUnknown
	stateOnline  nodeState = mdl.StateOnline
	stateOffline nodeState = mdl.StateOffline
	stateFailed  nodeState = mdl.StateFailed
	stateMuted   nodeState = mdl.StateMuted
)

// defaultCallsign returns the placeholder identity for a node we
// have no NodeInfo for.
//
//   - shortName is the last 4 hex digits of the node number
//     (lowercase). Every Meshtastic radio computes this same value
//     locally — it's a property of the node number, not a claim
//     about the user — which is why iOS / Android / meshtasticd all
//     show "c7f7" for the same peer. Putting it in the [shortname]
//     badge lets the user tab-complete against the same identifier
//     they hear other operators use over the air.
//
//   - longName stays "node 0x<hex>" — the full node ID. We
//     deliberately do NOT synthesize "Meshtastic <shortname>" here,
//     even though the firmware seeds that string when the owner
//     field is unset. We don't actually know if this peer kept the
//     factory default; claiming "Meshtastic c7f7" as their
//     longname would put a name in their mouth they may not have
//     chosen. The hex form is honest about what we know (just the
//     node ID) and is consistent with how /whois used to label the
//     row before we synthesized anything.
func defaultCallsign(nodeNum uint32) (longName, shortName string) {
	shortName = fmt.Sprintf("%04x", nodeNum&0xFFFF)
	longName = fmt.Sprintf("node 0x%x", nodeNum)
	return
}

// sortMode controls the nodes-overlay grid order. Cycled by the `s`
// nav-mode key; label() drives the "(sort: heard)" hint next to the
// pane title.
type sortMode int

const (
	sortByLastHeard sortMode = iota
	sortByName
	sortByState
)

func (s sortMode) label() string {
	switch s {
	case sortByName:
		return "name"
	case sortByState:
		return "state"
	default:
		return "heard"
	}
}

// nodeLastHeard returns the display string for "how long ago we
// heard this peer." Derived from LastHeardAt when set; falls back to
// the stored n.LastHeard for rows without an absolute timestamp
// (pre-backfill rows).
func nodeLastHeard(n *nodeItem) string {
	if n.LastHeardAt.IsZero() {
		return n.LastHeard
	}
	age := time.Since(n.LastHeardAt)
	if age < time.Minute {
		// "<1m" composes naturally with the " ago" suffix every
		// caller already appends, unlike "now ago" which reads
		// ungrammatical. Sub-minute granularity is below what the
		// mesh gives us anyway (RF latency + decode + redraw).
		return "<1m"
	}
	return humanDuration(age)
}

// isIgnored reports whether the given chat row's "from" string maps
// to a callsign on /ignore's list. Compares lowercased so case-only
// differences don't slip through, and uses HasPrefix so the chat
// row's "[shortname] longname" rendering still matches when the
// ignored entry is the longname alone — same loose-match rule
// /whois lookup uses.
func (m model) isIgnored(from string) bool {
	if len(m.Ignored) == 0 || from == "" {
		return false
	}
	low := strings.ToLower(from)
	for k := range m.Ignored {
		if k == "" {
			continue
		}
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// myCallsign returns the call to use for "me" in outbound messages,
// the status bar, etc. Live mode: look up our own node by MyNodeNum
// in the NodeDB.
func (m model) myCallsign() string {
	if m.MyNodeNum == 0 {
		return "—" // MyNodeInfo hasn't arrived yet
	}
	if idx, ok := m.NodesByNum[m.MyNodeNum]; ok && idx < len(m.Nodes) {
		return m.Nodes[idx].Callsign
	}
	return fmt.Sprintf("node 0x%x", m.MyNodeNum)
}

// myShortName returns our own Meshtastic shortname (4-ish char
// badge) — the tight identifier that fits on a radio OLED and
// matches what the phone app shows next to the longname. Live mode
// looks up our own nodeItem. Empty when we don't know yet.
func (m model) myShortName() string {
	if n := m.myNode(); n != nil {
		return n.ShortName
	}
	return ""
}

// myNode returns a pointer to our own node record — works in both
// live mode since MyInfo + NodeInfo stream populates it.
// Returns nil only when MyNodeInfo hasn't arrived yet on a live radio.
func (m model) myNode() *nodeItem {
	if m.MyNodeNum == 0 {
		return nil
	}
	if idx, ok := m.NodesByNum[m.MyNodeNum]; ok && idx < len(m.Nodes) {
		return &m.Nodes[idx]
	}
	return nil
}

// nodeNumOf resolves a user-supplied callsign to a node num, trying
// exact / prefix / substring case-insensitive matches in that order.
// Returns 0 if nothing matches. Parses Meshtastic "!<hex>" and
// "0x<hex>" notation first so an unambiguous id always short-circuits
// the fuzzy path — critical for disambiguating three radios that
// share a longname.
func (m *model) nodeNumOf(callsign string) uint32 {
	target := strings.ToLower(strings.TrimSpace(callsign))
	if num, ok := parseNodeHex(target); ok {
		if _, exists := m.NodesByNum[num]; exists {
			return num
		}
		// still return the num even if not in m.Nodes so callers
		// can see we parsed it
		return num
	}
	for num, idx := range m.NodesByNum {
		if idx < len(m.Nodes) && strings.ToLower(m.Nodes[idx].Callsign) == target {
			return num
		}
	}
	for num, idx := range m.NodesByNum {
		if idx < len(m.Nodes) && strings.HasPrefix(strings.ToLower(m.Nodes[idx].Callsign), target) {
			return num
		}
	}
	for num, idx := range m.NodesByNum {
		if idx < len(m.Nodes) && strings.Contains(strings.ToLower(m.Nodes[idx].Callsign), target) {
			return num
		}
	}
	return 0
}

// parseNodeHex recognises the two Meshtastic node-id spellings:
// "!<hex>" (canonical ! notation the phone app uses) and "0x<hex>"
// (our own "node 0x…" fallback). Returns the node num and true on a
// successful parse.
func parseNodeHex(s string) (uint32, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "!") {
		s = s[1:]
	} else if strings.HasPrefix(strings.ToLower(s), "0x") {
		s = s[2:]
	} else if strings.HasPrefix(strings.ToLower(s), "node 0x") {
		s = s[len("node 0x"):]
	} else {
		return 0, false
	}
	if s == "" {
		return 0, false
	}
	var n uint64
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= '0' && r <= '9':
			n = n<<4 | uint64(r-'0')
		case r >= 'a' && r <= 'f':
			n = n<<4 | uint64(r-'a'+10)
		default:
			return 0, false
		}
		if n > 0xFFFFFFFF {
			return 0, false
		}
	}
	return uint32(n), true
}

// lookupNode resolves a user-supplied callsign to a nodeItem. Tries
// three matches in order:
//
//  1. Exact case-insensitive — fast path
//  2. Prefix — "/whois KC7XYZ" matches "KC7XYZ 🦀"
//  3. Substring — "/whois rural" matches "Rural Signal 📡"
//
// Callsigns in Meshtastic often carry trailing emoji / badges / qth
// suffixes, so the flexibility is important for ergonomics. Every
// argumented ham command routes through this so we build reports
// from actual telemetry, never from placeholder text.
func (m *model) lookupNode(callsign string) *nodeItem {
	if callsign == "" {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(callsign))
	// Meshtastic node-id notation lands here straight from tab
	// completion's collision-disambiguation path — "!<hex>" means
	// "exactly this radio, don't fuzzy-match". Resolve via
	// NodesByNum so three radios sharing a longname each address
	// uniquely.
	if num, ok := parseNodeHex(target); ok {
		if idx, mapped := m.NodesByNum[num]; mapped && idx < len(m.Nodes) {
			return &m.Nodes[idx]
		}
		return nil
	}
	for i := range m.Nodes {
		if strings.ToLower(m.Nodes[i].Callsign) == target {
			return &m.Nodes[i]
		}
	}
	for i := range m.Nodes {
		if strings.HasPrefix(strings.ToLower(m.Nodes[i].Callsign), target) {
			return &m.Nodes[i]
		}
	}
	for i := range m.Nodes {
		if strings.Contains(strings.ToLower(m.Nodes[i].Callsign), target) {
			return &m.Nodes[i]
		}
	}
	return nil
}

// whoisHops renders the hop count for a /whois block. "self" gets a
// dedicated label so the line doesn't read "0 (direct)" which would
// imply a remote-but-direct peer; remote peers print the cached
// LastHops count, with 0 surfaced as "direct" so the user doesn't
// have to remember that 0 means "we hear them on RF without a relay".
// Falls back to "—" when no packet has carried a hop count yet
// (cold-start NodeDB drains where the User packet arrives but no
// MeshPacket has been routed yet).
func whoisHops(n *nodeItem, isSelf bool) string {
	if isSelf {
		return "self (we are the origin)"
	}
	if n.LastHops == 0 {
		return "direct (no relay)"
	}
	return fmt.Sprintf("%d (via %d intermediate)", n.LastHops, n.LastHops)
}

// signalReport renders the real-telemetry signal report for a node
// using its most recently heard packet's SNR/RSSI/hops. Used by /rs,
// /cqr, /ping — anywhere we'd otherwise fake a "copy 9/9" line.
func signalReport(n *nodeItem) string {
	parts := []string{}
	if n.LastHops > 0 {
		parts = append(parts, fmt.Sprintf("hop %d", n.LastHops))
	}
	if n.LastSNR != "" {
		parts = append(parts, fmt.Sprintf("SNR %s dB", n.LastSNR))
	}
	if n.LastRSSI != "" {
		parts = append(parts, fmt.Sprintf("RSSI %s dBm", n.LastRSSI))
	}
	if len(parts) == 0 {
		return "no telemetry yet"
	}
	return strings.Join(parts, ", ")
}
