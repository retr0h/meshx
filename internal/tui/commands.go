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

// commands.go wires the entire slash-command surface:
//
//   - executeCommand — the dispatcher ham / messaging / overlay verbs
//     all route through.
//   - sendBang / sendBangReply / systemLine / systemBlock — the four
//     primitives every slash handler uses to emit a user-visible row.
//   - newTextToRadio / newAdminSetOwner / setOwner — ToRadio envelope
//     builders + the AdminMessage write path used by /nick and /tag.
//   - small helpers (activate, actOnSelectedNode, ackWord,
//     currentChannelIndex, replyTargetFor) that commands lean on.
//
// Model / Update / render surface stays in app.go and ui.go.

package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
	"github.com/retr0h/meshx/internal/version"
)

// Channel role string constants. The pump stringifies pb.Channel_Role
// at the mdl.ChannelInfo boundary (FromRadio_Channel decoder); these
// keep the comparison sites in this file from being typo-bait.
const (
	roleDisabled  = "DISABLED"
	rolePrimary   = "PRIMARY"
	roleSecondary = "SECONDARY"
)

func (m *model) sendPlainMessage(text string) {
	m.sendPlainReply(text, 0)
}

// sendDM sends a directed-message TEXT_MESSAGE_APP to a specific
// peer with an optional Data.reply_id for threading. On the wire
// it's the same packet as sendPlainReply — the only difference is
// MeshPacket.to is the peer's node num instead of 0xFFFFFFFF. The
// local row is recorded with ToNum set so the renderer + tab-strip
// can distinguish DMs from broadcasts.
//
// targetNum=0 is a programming error (caller didn't resolve the
// peer); guard with a flash rather than letting it broadcast.
func (m *model) sendDM(targetNum uint32, targetCall, text string, replyToID uint32) {
	if targetNum == 0 {
		m.flash = "/msg: unknown peer"
		return
	}
	channel := int(m.currentChannelIndex())
	pid, _ := m.session.Send(mdl.SendText{
		Channel: channel,
		Text:    text,
		ReplyID: replyToID,
		ToNum:   targetNum,
	})
	res := m.session.RecordOutbound(radio.RecordOutboundOptions{
		Channel:  channel,
		Text:     text,
		ReplyID:  replyToID,
		PacketID: pid,
		ToNum:    targetNum,
	})
	if res.Index >= 0 {
		m.selectedMsg = res.Index
	}
	m.flash = fmt.Sprintf("DM sent to %s", targetCall)
}

// sendPlainReply is sendPlainMessage with an optional Data.reply_id
// for threading. Used by /reply, /me — anything that's semantically
// "regular chat with a directed flavor," NOT a /bang command. Routes
// through the same TEXT_MESSAGE_APP path sendBang uses; the only
// difference is msg.Bang stays empty so the chat row renders with
// the magenta `›` "mine" marker instead of the yellow `*` bang flag.
//
// On a DM tab this becomes a unicast to the active peer — keeps the
// `r` reply key + /reply command honest about where the message is
// going (without this DM-tab `r reply` would broadcast back to the
// channel with the peer's name prefixed).
func (m *model) sendPlainReply(text string, replyToID uint32) {
	if m.currentDMNum != 0 {
		call := dmCallsignFor(m, m.currentDMNum)
		m.sendDM(m.currentDMNum, call, text, replyToID)
		return
	}
	channel := int(m.currentChannelIndex())
	pid, _ := m.session.Send(mdl.SendText{
		Channel: channel,
		Text:    text,
		ReplyID: replyToID,
	})
	res := m.session.RecordOutbound(radio.RecordOutboundOptions{
		Channel:  channel,
		Text:     text,
		ReplyID:  replyToID,
		PacketID: pid,
	})
	if res.Index >= 0 {
		m.selectedMsg = res.Index
	}
	m.flash = fmt.Sprintf("sent in %s", m.CurrentChannel)
}

// setOwner validates the desired longname / shortname and sends an
// AdminMessage.SetOwner to the radio. `which` is "long" or "short"
// and controls which field the user is targeting — the other field
// is carried through unchanged from the current config (firmware
// overwrites the whole User record, so we have to round-trip both).
// Called by /nick and /tag. Returns a tea.Cmd for consistency with
// the dispatcher's expected shape; today it's always nil.
func (m *model) setOwner(longName, shortName, which string) tea.Cmd {
	if m.session.PumpHandle() == nil {
		m.flash = "/" + which + "name needs a live radio connection"
		return nil
	}
	// Default for empty-input handling matches the old TUI behavior:
	// /nick with no arg → usage; /tag with no arg → usage. The
	// session's UpdateConfig validates byte caps but treats an empty
	// supplied string as invalid (1..max). Catch the no-arg case up
	// front for the user-friendly usage message.
	target := strings.TrimSpace(longName)
	if which == "short" {
		target = strings.TrimSpace(shortName)
	}
	if target == "" {
		if which == "short" {
			m.flash = "usage: /tag <1-4 chars or emoji>"
		} else {
			m.flash = "usage: /nick <longname>"
		}
		return nil
	}
	// Only the touched field gets sent; UpdateConfig coalesces and
	// preserves the omitted half of the User record from State.
	req := radio.UpdateConfigRequest{}
	switch which {
	case "long":
		req.LongName = &longName
	case "short":
		req.ShortName = &shortName
	}
	if _, err := m.session.UpdateConfig(req); err != nil {
		m.flash = fmt.Sprintf("/%sname: %v", which, err)
		return nil
	}
	switch which {
	case "long":
		m.systemLine(
			fmt.Sprintf("nick → %s (radio will re-broadcast NodeInfo on next cycle)", longName),
		)
	case "short":
		m.systemLine(
			fmt.Sprintf("tag → %s (radio will re-broadcast NodeInfo on next cycle)", shortName),
		)
	}
	return nil
}

// meshtasticChannelSlots is the firmware's hard cap on simultaneous
// channels. Slot 0 is always PRIMARY; 1..7 are SECONDARY. /channel
// new + add allocate into the first DISABLED slot >= 1; PRIMARY is
// off-limits because the radio refuses to operate without one.
//
// Slot allocation, free-slot lookup, name resolution all live on
// *radio.Session — see internal/radio/ops_channels.go. The TUI just
// dispatches via m.session.X and surfaces flash / system-block
// feedback.

// channelShare round-trips a local channel back into a meshtastic://
// URL and renders it as an ASCII QR for in-person scanning. The PSK
// in m.Channels[idx].PSK was sourced from the radio's NodeDB dump
// (see applyChannel) — never read from disk, never reads from disk.
//
// The QR is the safest hand-off path: the bytes go terminal →
// photons → recipient's camera with no network in the loop. Anything
// else (DM, screenshot pasted in chat) means the PSK exists on
// another system you don't control. We surface a "verify the person
// scanning this can see the screen and only that person" reminder so
// users don't routinely paste the URL into group chats.
func (m *model) channelShare(typed string) tea.Cmd {
	if m.session.PumpHandle() == nil {
		m.flash = "/channel share needs a live radio connection"
		return nil
	}
	idx := m.session.LookupChannelByName(typed)
	if idx < 0 {
		m.flash = fmt.Sprintf("/channel share: no channel matching %q", typed)
		return nil
	}
	// Primary-without-PSK is allowed by the ops layer (HTTP exposes it
	// at GET /channels/0/share). The TUI refuses interactively because
	// sharing default LongFast as a QR has no recipient who needs it.
	if c := m.Channels[idx]; c.Role == rolePrimary && len(c.PSK) == 0 {
		m.flash = "/channel share: the default channel is on every radio — nothing to share"
		return nil
	}
	res, err := m.session.ShareChannel(radio.ShareChannelRequest{Index: idx})
	if err != nil {
		m.flash = fmt.Sprintf("/channel share failed: %v", err)
		return nil
	}
	qr, err := renderQRASCII(res.ShareURL)
	if err != nil {
		m.flash = fmt.Sprintf("/channel share: qr render failed: %v", err)
		return nil
	}
	header := fmt.Sprintf("/channel share — %s", res.Name)
	qrLines := strings.Split(qr, "\n")
	lines := make([]string, 0, len(qrLines)+4)
	lines = append(
		lines,
		"scan with the Meshtastic app to join (in-person only — the PSK is in this QR)",
		"",
	)
	lines = append(lines, qrLines...)
	lines = append(lines, "", "url: "+res.ShareURL)
	m.systemBlock(header, lines...)
	m.flash = fmt.Sprintf("share QR for %s — visible in log", res.Name)
	return nil
}

// channelNew creates a fresh secondary channel with a randomly
// generated 32-byte AES256 PSK and pushes it to the first free slot.
// The PSK never lands on disk — meshX has no channels table; the
// bytes round-trip through pump → channelItem.PSK where they wait in
// RAM for /channel share to wrap them in a meshtastic:// URL.
//
// Name is enforced ≤ 11 bytes per the proto comment ("Less than 12
// bytes") — Meshtastic packs the name into the URL and short names
// keep the QR small and the channel selector readable.
//
// We print a SHA-256 fingerprint (first 8 hex of the PSK hash) so the
// user can verbally confirm key parity with whoever they share it
// with — "my fingerprint is a3f2c9b1, what's yours?" — without
// reading the raw PSK bytes aloud. Same convention SSH uses for host
// key fingerprints.
func (m *model) channelNew(name string) tea.Cmd {
	if m.session.PumpHandle() == nil {
		m.flash = "/channel new needs a live radio connection"
		return nil
	}
	if strings.TrimSpace(strings.TrimPrefix(name, "#")) == "" {
		m.flash = "usage: /channel new <name>"
		return nil
	}
	res, err := m.session.MintChannel(radio.MintChannelRequest{Name: name})
	if err != nil {
		m.flash = fmt.Sprintf("/channel new: %v", err)
		return nil
	}
	display := "*" + res.Name + "*"
	fp := pskFingerprint(res.PSK)
	m.systemBlock(
		fmt.Sprintf("/channel new — %s created at slot %d", display, res.Index),
		"PSK: 32 random bytes (AES256), RAM-only — never written to disk",
		fmt.Sprintf("fingerprint: %s   ← read aloud to verify parity with the recipient", fp),
		fmt.Sprintf("share:       /channel share %s", res.Name),
	)
	m.flash = fmt.Sprintf("created %s (fp %s)", display, fp)
	return nil
}

// pskFingerprint returns the first 8 hex chars of SHA-256(psk) — a
// short, human-readable identifier the user can read aloud to confirm
// key parity with a recipient ("my fingerprint is a3f2c9b1, what's
// yours?"). Same convention SSH uses for host key fingerprints. The
// PSK itself is never displayed — only the hash. TUI-only: the HTTP
// API never exposes the PSK to clients (it's already encoded in the
// share URL).
func pskFingerprint(psk []byte) string {
	sum := sha256.Sum256(psk)
	return hex.EncodeToString(sum[:4])
}

// channelDel disables a channel slot via AdminMessage_SetChannel
// with role=DISABLED and nil settings — the radio frees the slot and
// wipes the PSK. Refuses to delete the PRIMARY (slot 0) because the
// firmware requires one to operate; the user can /channel rename or
// /config the primary instead.
//
// No confirmation prompt — the cost of an accidental /channel del is
// just /channel add the URL again (if you have it), and forcing y/n
// on every channel-management command would feel patronizing for what
// is fundamentally a local-state edit. If we ever ship /channel
// backup, we can add a "deleted N channels" undo window.
func (m *model) channelDel(typed string) tea.Cmd {
	if m.session.PumpHandle() == nil {
		m.flash = "/channel del needs a live radio connection"
		return nil
	}
	idx := m.session.LookupChannelByName(typed)
	if idx < 0 {
		m.flash = fmt.Sprintf("/channel del: no channel matching %q", typed)
		return nil
	}
	if m.Channels[idx].Role == rolePrimary {
		m.flash = "/channel del: cannot delete the primary channel — use /config to rename"
		return nil
	}
	res, err := m.session.DeleteChannel(radio.DeleteChannelRequest{Index: idx})
	if err != nil {
		m.flash = fmt.Sprintf("/channel del: %v", err)
		return nil
	}
	if m.CurrentChannel == res.Name {
		// User deleted the channel they were on. Snap back to the
		// primary so the input bar has a valid target.
		for _, c := range m.Channels {
			if c.Role == rolePrimary {
				m.CurrentChannel = c.Name
				break
			}
		}
	}
	m.systemLine(fmt.Sprintf("channel %s deleted (slot %d freed)", res.Name, idx))
	m.flash = fmt.Sprintf("deleted %s", res.Name)
	return nil
}

// channelAdd accepts a meshtastic://e/#... or
// https://meshtastic.org/e/#... URL, decodes the embedded ChannelSet,
// and pushes each channel into the first free secondary slot via
// AdminMessage_SetChannel. Skips channels whose name already exists
// on the radio (additive only — never overwrites). Refuses to push
// into slot 0 (PRIMARY) so a malformed share link can't nuke the
// user's primary channel.
func (m *model) channelAdd(rawURL string) tea.Cmd {
	if m.session.PumpHandle() == nil {
		m.flash = "/channel add needs a live radio connection"
		return nil
	}
	res, err := m.session.ImportChannel(radio.ImportChannelRequest{URL: rawURL})
	if err != nil {
		m.flash = "/channel add: " + err.Error()
		return nil
	}
	if len(res.Imported) == 0 && len(res.Skipped) == 0 {
		m.flash = "/channel add: nothing to do"
		return nil
	}
	summary := make([]string, 0, len(res.Imported)+len(res.Skipped))
	for _, ic := range res.Imported {
		summary = append(summary, fmt.Sprintf("add:  %s → slot %d", ic.Name, ic.Index))
	}
	for _, sc := range res.Skipped {
		name := sc.Name
		if name == "" {
			name = "<empty name>"
		}
		summary = append(summary, fmt.Sprintf("skip: %q — %s", name, sc.Reason))
	}
	header := fmt.Sprintf(
		"/channel add — %d added, %d skipped",
		len(res.Imported), len(res.Skipped),
	)
	m.systemBlock(header, summary...)
	if len(res.Imported) > 0 {
		m.flash = fmt.Sprintf("added %d channel%s", len(res.Imported), plural(len(res.Imported)))
	} else {
		m.flash = "no channels added — see log"
	}
	return nil
}

// pingTimeoutSeconds bounds how long /ping waits for the REPLY_APP
// echo before declaring the request lost. Same 30s ballpark /tr
// uses — enough for a multi-hop round trip on slow modem presets,
// not so long the user thinks the command silently no-opped.
const pingTimeoutSeconds = 30

func pingTimeoutCmd(packetID uint32) tea.Cmd {
	return tea.Tick(pingTimeoutSeconds*time.Second, func(time.Time) tea.Msg {
		return pingTimeoutMsg{packetID: packetID}
	})
}

// tracerouteTimeoutSeconds bounds how long /tr waits for a
// TRACEROUTE_APP reply before declaring the request lost. 30s covers
// a 6-hop round trip on a slow LongFast mesh with retries — same
// ballpark the official Meshtastic clients use. tracerouteTimeoutCmd
// returns a tea.Cmd that fires tracerouteTimeoutMsg after the
// deadline; the handler short-circuits if radio.PendingTraceroute already
// resolved or got replaced by a newer /tr.
const tracerouteTimeoutSeconds = 30

func tracerouteTimeoutCmd(packetID uint32) tea.Cmd {
	return tea.Tick(tracerouteTimeoutSeconds*time.Second, func(time.Time) tea.Msg {
		return tracerouteTimeoutMsg{packetID: packetID}
	})
}

// resetConfigDraft snapshots the live radio state into m.cfgDraft so
// the /config panel opens populated with current values. Called from
// openOverlay(overlayConfig) so re-opening the panel always starts
// clean — any unsaved edits from a prior session are discarded the
// moment the panel closes (a future "save on close" would invert
// this, but the current design is "explicit Ctrl+S or it didn't
// happen").
func (m *model) resetConfigDraft() {
	m.cfgDraft = configDraft{
		buzzer:    m.RadioBuzzerEnabled,
		longName:  m.myCallsign(),
		shortName: m.myShortName(),
	}
	m.cfgEditing = ""
	m.cfgConfirmDiscard = false
}

// configDraftDirty reports whether the draft has any field that
// differs from the live state. Drives the dirty-marker in the panel
// header + each row, and gates the Esc-on-dirty discard prompt.
func (m model) configDraftDirty() bool {
	if m.cfgDraft.buzzer != m.RadioBuzzerEnabled {
		return true
	}
	if m.cfgDraft.longName != m.myCallsign() {
		return true
	}
	if m.cfgDraft.shortName != m.myShortName() {
		return true
	}
	return false
}

// commitConfigDraft fires the AdminMessage(s) needed to make the radio
// match the draft, and persists the local mirrors. Walks the diff so
// rows that didn't change don't generate wire traffic. Returns the
// number of changes applied so the caller can flash a sane summary.
//
// Validates string fields against the firmware's byte limits before
// touching the wire — same caps setOwner enforces — so a bad longname
// rejects in-panel without the radio seeing it.
func (m *model) commitConfigDraft() int {
	if m.session.PumpHandle() == nil {
		m.flash = "/config: save needs a live radio connection"
		return 0
	}
	// Build a sparse UpdateConfigRequest — only fields that diverge
	// from current state get pointers. The session method coalesces
	// owner fields into one SetOwner dispatch and rejects oversize
	// strings with a 400, so this loop just stages the diff.
	req := radio.UpdateConfigRequest{}
	if m.cfgDraft.longName != m.myCallsign() {
		ln := m.cfgDraft.longName
		req.LongName = &ln
	}
	if m.cfgDraft.shortName != m.myShortName() {
		sn := m.cfgDraft.shortName
		req.ShortName = &sn
	}
	if m.cfgDraft.buzzer != m.RadioBuzzerEnabled {
		b := m.cfgDraft.buzzer
		req.Buzzer = &b
	}
	if req.LongName == nil && req.ShortName == nil && req.Buzzer == nil {
		m.flash = "/config: no changes to save"
		return 0
	}
	res, err := m.session.UpdateConfig(req)
	if err != nil {
		m.flash = fmt.Sprintf("/config: %v", err)
		return 0
	}
	// Local-mirror updates the TUI keeps for instant feedback. The
	// authoritative state lands when the radio re-broadcasts NodeInfo /
	// ConfigComplete; these are just for the next render before that.
	if req.Buzzer != nil {
		m.RadioBuzzerEnabled = *req.Buzzer
		v := "on"
		if !*req.Buzzer {
			v = "off"
		}
		m.session.PutSetting(m.RadioID, "radio_buzzer", v)
	}
	changes := len(res.Applied)
	m.flash = fmt.Sprintf("/config: %d change%s saved — radio updating", changes, plural(changes))
	m.systemLine(fmt.Sprintf("config: committed %d change%s", changes, plural(changes)))
	return changes
}

// buildVersionLines returns the rows /version dumps as a systemBlock.
// Reads from BuildInfo() (the same goversion.Info `meshx version`
// JSON-prints) so the in-app surface and the CLI surface report
// identical data — caarlos0/go-version backfills sensible defaults
// from runtime/debug.ReadBuildInfo when goreleaser ldflags weren't
// applied (e.g. plain `go build`), so we get the commit SHA + dirty
// flag for free without forking the discovery logic.
//
// Also includes the radio's firmware version when known, so the user
// can see at a glance whether their firmware is current.
func buildVersionLines(m *model) []string {
	v := version.BuildInfo()
	lines := []string{
		fmt.Sprintf("meshx:    %s", v.GitVersion),
	}
	if v.GitCommit != "" {
		commit := v.GitCommit
		if len(commit) > 7 {
			commit = commit[:7]
		}
		if v.GitTreeState == "dirty" {
			commit += "-dirty"
		}
		lines = append(lines, fmt.Sprintf("commit:   %s", commit))
	}
	if v.BuildDate != "" {
		lines = append(lines, fmt.Sprintf("built:    %s", v.BuildDate))
	}
	if v.BuiltBy != "" {
		lines = append(lines, fmt.Sprintf("by:       %s", v.BuiltBy))
	}
	lines = append(lines, fmt.Sprintf("go:       %s", v.GoVersion))
	if m.RadioFirmware != "" {
		lines = append(lines, fmt.Sprintf("Firmware: %s", m.RadioFirmware))
	} else {
		lines = append(lines, "Firmware: (waiting on Metadata packet)")
	}
	return lines
}

// plural returns "s" when n != 1 — micro-helper to keep config save
// flash messages grammatical without inline ternaries littering the
// commit path.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// currentChannelIndex maps m.CurrentChannel back to the Meshtastic
// channel index used on the wire. Defaults to 0 (PRIMARY) when the
// channel name isn't in our list.
func (m model) currentChannelIndex() uint32 {
	for i, c := range m.Channels {
		if c.Name == m.CurrentChannel {
			return uint32(i)
		}
	}
	return 0
}

// timeNowHHMM returns the current wall time in HH:MM for message
// timestamps. Extracted so tests can override if needed.

func (m *model) actOnSelectedNode(fn func(*nodeItem)) {
	if m.focused != paneNodes {
		return
	}
	sorted := m.sortedNodes()
	if m.selectedNd < 0 || m.selectedNd >= len(sorted) {
		return
	}
	target := sorted[m.selectedNd].Callsign
	for i := range m.Nodes {
		if m.Nodes[i].Callsign == target {
			fn(&m.Nodes[i])
			return
		}
	}
}

// activate is the "open/select" action — Enter and Space in normal mode.
// Meaning depends on which pane is focused:
//   - channels: switch the messages pane to that channel
//   - nodes:    show whois / node info flash
//   - messages: expand selected message (hop, SNR, RSSI, hex id)
func (m *model) activate() tea.Cmd {
	switch m.focused {
	case paneConfig:
		entries := m.configEntries()
		if m.selectedCfg < 0 || m.selectedCfg >= len(entries) {
			return nil
		}
		e := entries[m.selectedCfg]
		switch e.kind {
		case cfgEntryString:
			// Swap into inline-edit mode. The Component checks
			// m.cfgEditing and renders the textinput in place of the
			// static value cell, so the cursor lives right where the
			// row's draft value already does. Pre-fill with the
			// current draft so a "small tweak" doesn't require
			// retyping the whole field.
			m.cfgEditing = e.field
			m.cfgEditInput.SetValue(e.value)
			m.cfgEditInput.CursorEnd()
			m.mode = modeConfigEdit
			return m.cfgEditInput.Focus()
		case cfgEntryToggle:
			if e.action != nil {
				e.action(m)
			}
			return nil
		}
		// Read-only row — Enter is a no-op. selectableConfig...Indices
		// already prevents the cursor from parking here, but guard.
		return nil
	case paneChannels:
		if m.selectedCh < len(m.Channels) {
			c := m.Channels[m.selectedCh]
			m.CurrentChannel = c.Name
			m.Channels[m.selectedCh].Unread = 0
			m.flash = fmt.Sprintf("switched to %s", c.Name)
			// Land on the input bar in the new channel — same as
			// /join. Without this the user is stuck in nav mode on
			// the (now-closed) channels overlay and has to ESC to
			// type. Reuses closeOverlayToInput so overlay state,
			// focused pane, mode flag, and textinput.Focus() all
			// flip together — matches every other "we just acted on
			// the user's selection, now hand them back the keyboard"
			// transition (revealMessages, etc.).
			return m.closeOverlayToInput()
		}
	case paneNodes:
		sorted := m.sortedNodes()
		if m.selectedNd < len(sorted) {
			n := sorted[m.selectedNd]
			hw := n.HwModel
			if hw == "" {
				hw = "?"
			}
			fw := n.Firmware
			if fw == "" {
				fw = "?"
			}
			m.flash = fmt.Sprintf(
				"%s  ·  %s  ·  fw %s  ·  last heard %s  ·  %s",
				n.Callsign, hw, fw, nodeLastHeard(&n), n.CurrentState(),
			)
		}
	case paneMessages:
		if m.selectedMsg < len(m.Messages) {
			msg := m.Messages[m.selectedMsg]
			switch {
			case msg.Status == mdl.StatusSystem:
				m.flash = "system message — no metadata"
			case msg.Mine:
				m.flash = fmt.Sprintf("to %s  ·  hop %d  ·  ACK %s",
					m.CurrentChannel, msg.Hops, ackWord(msg.Status))
			default:
				parts := []string{"from " + msg.From}
				if msg.Hops > 0 {
					parts = append(parts, fmt.Sprintf("hop %d", msg.Hops))
				}
				if msg.SNR != "" {
					parts = append(parts, "SNR "+msg.SNR+" dB")
				}
				m.flash = strings.Join(parts, "  ·  ")
			}
		}
	}
	return nil
}

func ackWord(status mdl.MessageStatus) string {
	switch status {
	case mdl.StatusAck:
		return "ok"
	case mdl.StatusFail:
		return "timeout"
	default:
		return "pending"
	}
}

func (m *model) executeCommand(raw string) tea.Cmd {
	if raw == "" {
		return nil
	}
	// Split into verb + rest for arg-taking commands.
	verb := raw
	rest := ""
	if sp := strings.IndexByte(raw, ' '); sp >= 0 {
		verb = raw[:sp]
		rest = strings.TrimSpace(raw[sp+1:])
	}

	switch verb {
	case "q", "quit", "exit":
		return tea.Quit

	case "pin":
		// Toggle pin on the most recent ephemeral notice. "Ephemeral"
		// = has an expireAt stamp (command-triggered notice, not
		// splash / storage / chat). Typed from input with no explicit
		// selection, so the heuristic is "the thing the user most
		// recently ran."
		idx := m.lastEphemeralNoticeIdx()
		if idx < 0 {
			m.flash = "/pin: nothing pinnable in the log"
			return nil
		}
		pinned := !m.Messages[idx].Pinned
		m.toggleNoticePin(idx)
		if pinned {
			m.flash = "notice pinned — timer paused"
		} else {
			m.flash = "notice unpinned — timer resumed"
		}

	// Ham-radio bang shortcuts — quick-command shorthands that compose
	// and send the underlying !bang message. Keeps the protocol payload
	// visible as normal message text so every other Meshtastic client
	// sees it as plain chat.

	case "cq":
		// Ham-customary "via <rig/app>" suffix on the beacon so
		// anyone copying the CQ knows what client the caller runs.
		// Only /cq carries this tag — routine chat + reply verbs
		// stay clean so a 237-byte LoRa payload isn't wasted on
		// attribution on every packet.
		call := m.myCallsign()
		body := fmt.Sprintf("CQ CQ CQ de %s via %s — testing signals, please ack", call, clientTag)
		if rest != "" {
			body = fmt.Sprintf("CQ de %s via %s %s", call, clientTag, rest)
		}
		m.sendBang("/cq", body)
		m.flash = "/cq broadcast sent"
	case "cqr":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /cqr <callsign>  (or highlight their CQ in nav mode)"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no telemetry for %s — node unknown", target)
			return nil
		}
		m.sendBangReply("/cqr "+target, signalReport(n), m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!cqr %s — copy report sent (%s)", target, signalReport(n))
	case "rs":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /rs <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no telemetry for %s — node unknown", target)
			return nil
		}
		m.sendBangReply("/rs "+target, signalReport(n), m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!rs %s — %s", target, signalReport(n))
	case "73":
		// /73           → broadcast best-regards
		// /73 <call>    → directed "73 <call>" — aimed at a specific
		//                 operator you're signing off to cordially.
		//                 Threads via Data.reply_id to that operator's
		//                 most recent message when we have one.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/73", "73")
			m.flash = "!73 sent"
			return nil
		}
		m.sendBangReply("/73 "+target, "73 "+target, m.replyTargetFor(target))
		m.flash = "!73 " + target + " — best regards"
	case "88":
		m.sendBang("/88", "88")
		m.flash = "!88 sent"
	case "qsl":
		// /qsl           → broadcast acknowledgment
		// /qsl <call>    → directed "QSL <call>" — aimed at a specific
		//                  operator whose last transmission we copied.
		//                  Threads via Data.reply_id to that operator's
		//                  most recent message.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/qsl", "QSL")
			m.flash = "!qsl — acknowledged"
			return nil
		}
		body := "QSL " + target
		m.sendBangReply("/qsl "+target, body, m.replyTargetFor(target))
		m.flash = "!qsl " + target + " — copy confirmed"
	case "qth":
		// PRIVACY — /qth only transmits when the user runs it
		// explicitly, and only the coarse Maidenhead grid (~20 km
		// precision). Never exact lat/long.
		//
		// Two forms:
		//   /qth                → broadcast your own grid (from radio GPS)
		//   /qth <text>         → broadcast a custom QTH string
		//
		// To look up a PEER's QTH, use /whois <call> — keeps send vs.
		// query unambiguous.
		arg := strings.TrimSpace(rest)
		if arg == "" {
			if m.MyGrid == "" {
				m.flash = "no GPS fix — /qth <text> to send a custom QTH, or configure position on the radio"
				return nil
			}
			m.sendBang("/qth", "QTH: "+m.MyGrid)
			m.flash = "QTH: " + m.MyGrid
			return nil
		}
		m.sendBang("/qth", "QTH: "+arg)
		m.flash = "QTH: " + arg
	case "env":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /env <callsign>"
			return nil
		}
		nodeNum := m.nodeNumOf(target)
		if nodeNum == 0 {
			m.systemLine(fmt.Sprintf("env: no record of %s", target))
			return nil
		}
		n := m.lookupNode(target)
		env, ok := m.PeerEnv[nodeNum]
		if !ok {
			m.systemLine(fmt.Sprintf("env: %s has no environmental telemetry on file", n.Callsign))
			m.systemLine("     (only peers with temp/humidity/pressure sensors broadcast this)")
			return nil
		}
		var lines []string
		if env.Temperature != 0 {
			lines = append(lines, fmt.Sprintf("temp:     %.1f °C", env.Temperature))
		}
		if env.Humidity != 0 {
			lines = append(lines, fmt.Sprintf("humidity: %.0f %%", env.Humidity))
		}
		if env.Pressure != 0 {
			lines = append(lines, fmt.Sprintf("pressure: %.0f hPa", env.Pressure))
		}
		if env.Gas != 0 {
			lines = append(lines, fmt.Sprintf("gas:      %.0f Ω", env.Gas))
		}
		lines = append(lines, fmt.Sprintf("age:      %s ago", humanDuration(time.Since(env.At))))
		m.systemBlock(fmt.Sprintf("env %s", n.Callsign), lines...)

	case "qrz":
		// "Who is calling me?" — broadcast a prompt for identification.
		m.sendBang("/qrz", "QRZ? who's calling?")
		m.flash = "!qrz — asking for ID"
	case "qrm":
		// "You have man-made interference." Report to a station.
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /qrm <callsign>"
			return nil
		}
		m.sendBangReply(
			"/qrm "+target,
			"QRM — interference on your signal",
			m.replyTargetFor(target),
		)
		m.flash = fmt.Sprintf("!qrm %s — interference reported", target)
	case "qsb":
		// "Your signal is fading."
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /qsb <callsign>"
			return nil
		}
		m.sendBangReply("/qsb "+target, "QSB — signal fading, copy weak", m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!qsb %s — fade reported", target)
	case "sk":
		// Final sign-off — stronger than /73. "Signing off clear."
		// /sk           → broadcast SK
		// /sk <call>    → directed "SK <call>" — aimed at a specific
		//                 operator you're closing a contact with.
		//                 Threads via Data.reply_id to that operator's
		//                 most recent message.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/sk", "SK — clear and out 73")
			m.flash = "!sk — clear"
			return nil
		}
		body := "SK — clear and out 73, " + target
		m.sendBangReply("/sk "+target, body, m.replyTargetFor(target))
		m.flash = "!sk " + target + " — cleared"
	case "wx":
		// Weather at my QTH. Optional argument supplies the conditions;
		// without one we emit a placeholder so the user types their own.
		wx := rest
		if wx == "" {
			wx = "clear 55°F light wind"
		}
		m.sendBang("/wx", "wx: "+wx)
		m.flash = "wx: " + wx + " — broadcast"
	case "grid":
		// Just the Maidenhead locator — shorter / more data-friendly
		// than /qth which also names the city.
		grid := rest
		if grid == "" {
			grid = m.MyGrid
		}
		if grid == "" {
			m.flash = "no GPS fix — /grid <locator> to send a custom grid"
			return nil
		}
		m.sendBang("/grid", "grid: "+grid)
		m.flash = "grid: " + grid + " — broadcast"
	case "mesh":
		// Meshtastic-specific — summarize what the mesh looks like
		// from our vantage: number of nodes we can hear, by state.
		online, muted, offline := 0, 0, 0
		for i := range m.Nodes {
			switch m.Nodes[i].CurrentState() {
			case stateOnline:
				online++
			case stateMuted:
				muted++
			case stateOffline, stateFailed:
				offline++
			}
		}
		body := fmt.Sprintf("mesh view: %d online, %d muted, %d stale", online, muted, offline)
		m.sendBang("/mesh", body)
		m.flash = body
	case "k":
		// "Over — go ahead." Ragchew turn-taking.
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /k <callsign>"
			return nil
		}
		m.sendBangReply("/k "+target, "K — over, go ahead", m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!k %s — over to you", target)

	case "tr":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /tr <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("tr: node %s unknown", target))
			return nil
		}
		// No live wire traffic possible when the pump isn't up yet;
		// fall back to cached telemetry with a clear "no live path" tag.
		if m.session.PumpHandle() == nil {
			m.systemBlock(
				fmt.Sprintf("traceroute %s", n.Callsign),
				fmt.Sprintf("hops:   %d (cached)", n.LastHops),
				fmt.Sprintf("signal: %s", signalReport(n)),
				"note:   live traceroute needs a real radio connection",
			)
			return nil
		}
		// Self-traceroute is meaningless — firmware drops it.
		if n.NodeNum != 0 && n.NodeNum == m.MyNodeNum {
			m.systemLine("tr: that's you — /info for your own config")
			return nil
		}
		// One traceroute in flight at a time. Issuing a second /tr
		// while the first hasn't resolved would orphan the old
		// radio.PendingTraceroute (the new packetID overwrites the field
		// and the original timeout tick never finds a match). Refuse
		// loud rather than silently lose the prior request.
		if m.PendingTraceroute != nil {
			m.flash = fmt.Sprintf(
				"tr: already tracing %s — wait or it'll auto-timeout",
				m.PendingTraceroute.TargetCall,
			)
			return nil
		}
		res, err := m.session.Traceroute(radio.TracerouteRequest{TargetNum: n.NodeNum})
		if err != nil {
			m.flash = fmt.Sprintf("tr: %v", err)
			return nil
		}
		m.PendingTraceroute = &radio.PendingTraceroute{
			PacketID:    res.PacketID,
			TargetNum:   n.NodeNum,
			TargetCall:  n.Callsign,
			RequestedAt: time.Now(),
		}
		m.flash = fmt.Sprintf(
			"tr: tracing %s (waiting up to %ds)",
			n.Callsign, tracerouteTimeoutSeconds,
		)
		m.systemLine(fmt.Sprintf(
			"traceroute %s — request sent (id 0x%x), awaiting reply",
			n.Callsign, res.PacketID,
		))
		return tracerouteTimeoutCmd(res.PacketID)
	case "ping":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /ping <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("ping: node %s unknown", target))
			return nil
		}
		// Pinging ourselves yields meaningless telemetry (firmware
		// won't echo a packet back to its own node). Refuse with a
		// note rather than emitting a request that will silently
		// timeout.
		if n.NodeNum != 0 && n.NodeNum == m.MyNodeNum {
			m.systemLine("ping: that's you — /whois for your own config")
			return nil
		}
		// Offline fall back to cached telemetry.
		if m.session.PumpHandle() == nil {
			lines := []string{
				fmt.Sprintf("last heard: %s ago (cached)", nodeLastHeard(n)),
				fmt.Sprintf("signal:     %s", signalReport(n)),
				"note:       live ping needs a real radio connection",
			}
			if nodeNum := m.nodeNumOf(target); nodeNum != 0 {
				if pos, ok := m.PeerPositions[nodeNum]; ok && m.MyGrid != "" {
					if km := haversineKm(m.MyLatitude, m.MyLongitude, pos.Latitude, pos.Longitude); km > 0 {
						lines = append(
							lines,
							fmt.Sprintf("distance:   %.1f km", km),
						)
					}
				}
			}
			m.systemBlock(fmt.Sprintf("ping %s", n.Callsign), lines...)
			return nil
		}
		// One ping in flight at a time. Same shape as pendingTraceroute.
		if m.PendingPing != nil {
			m.flash = fmt.Sprintf(
				"ping: already pinging %s — wait or it'll auto-timeout",
				m.PendingPing.TargetCall,
			)
			return nil
		}
		res, err := m.session.Ping(radio.PingRequest{TargetNum: n.NodeNum})
		if err != nil {
			m.flash = fmt.Sprintf("ping: %v", err)
			return nil
		}
		m.PendingPing = &radio.PendingPing{
			PacketID:    res.PacketID,
			TargetNum:   n.NodeNum,
			TargetCall:  n.Callsign,
			RequestedAt: time.Now(),
		}
		m.flash = fmt.Sprintf(
			"ping: pinging %s (waiting up to %ds)",
			n.Callsign, pingTimeoutSeconds,
		)
		m.systemLine(fmt.Sprintf(
			"ping %s — request sent (id 0x%x), awaiting echo",
			n.Callsign, res.PacketID,
		))
		return pingTimeoutCmd(res.PacketID)
	case "w", "whois":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /whois <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("whois: no record of %s", target))
			return nil
		}
		hw := n.HwModel
		if hw == "" {
			hw = "unknown hw"
		}
		fw := n.Firmware
		nodeNum := m.nodeNumOf(target)
		isSelf := nodeNum != 0 && nodeNum == m.MyNodeNum
		// For our own node, fw lives on m.RadioFirmware (from
		// FromRadio.Metadata), not on the nodeItem — MyNodeInfo
		// doesn't carry firmware.
		if isSelf && fw == "" {
			fw = m.RadioFirmware
		}
		if fw == "" {
			fw = "?"
		}

		ghost := n.Unresolved

		var lines []string
		lines = append(lines, fmt.Sprintf("Name: %s", n.Callsign))
		if n.ShortName != "" {
			lines = append(lines, fmt.Sprintf("short:  %s", n.ShortName))
		}
		if nodeNum != 0 {
			lines = append(lines, fmt.Sprintf("id:     0x%x", nodeNum))
		}
		lines = append(lines, "")
		if ghost {
			lines = append(
				lines,
				"👻 no NodeInfo received for this peer",
				"  we've heard text packets from them but never their",
				"  User broadcast, so longname / hw / fw / position are",
				"  unknown. Their NodeInfo may arrive in the next",
				"  ~15 min, or try /sync to force a NodeDB re-dump.",
				"",
			)
		}
		lines = append(
			lines,
			fmt.Sprintf("hw:     %s", hw),
			fmt.Sprintf("fw:     %s", fw),
			fmt.Sprintf("heard:  %s ago", nodeLastHeard(n)),
			fmt.Sprintf("State: %s", n.CurrentState()),
			fmt.Sprintf("signal: %s", signalReport(n)),
			fmt.Sprintf("hops:   %s", whoisHops(n, isSelf)),
		)
		if isSelf && m.HasTelemetry {
			pct := "—"
			switch {
			case m.BatteryLevel > 100:
				pct = "pwr (USB / solar — no cell)"
			case m.BatteryLevel > 0:
				pct = fmt.Sprintf("%d%%", m.BatteryLevel)
			}
			if m.BatteryVoltage > 0 {
				lines = append(lines, fmt.Sprintf("battery: %s  %.2f V", pct, m.BatteryVoltage))
			} else {
				lines = append(lines, fmt.Sprintf("battery: %s", pct))
			}
			lines = append(lines, fmt.Sprintf("chanutl: %.1f%%", m.ChannelUtil))
		}
		if nodeNum != 0 {
			if pos, ok := m.PeerPositions[nodeNum]; ok {
				lines = append(
					lines,
					fmt.Sprintf("grid:   %s", pos.Grid),
					fmt.Sprintf(
						"coord:  %.5f, %.5f  alt %d m",
						pos.Latitude,
						pos.Longitude,
						pos.Altitude,
					),
					fmt.Sprintf("fix age: %s ago", humanDuration(time.Since(pos.At))),
				)
				if !isSelf && m.MyLatitude != 0 && m.MyLongitude != 0 {
					if km := haversineKm(m.MyLatitude, m.MyLongitude, pos.Latitude, pos.Longitude); km > 0 {
						lines = append(lines, fmt.Sprintf("dist:   %.1f km from you", km))
					}
				}
			}
		}
		lines = append(lines, "end of /whois")
		m.systemBlock(fmt.Sprintf("whois %s", n.Callsign), lines...)
	case "r", "reply":
		if rest == "" {
			target := m.selectedSender()
			if target == "" {
				m.flash = "usage: /reply <callsign> <text>"
				return nil
			}
			return m.prefillInput("/reply " + target + " ")
		}
		// /reply <call> <text>
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			return m.prefillInput("/reply " + rest + " ")
		}
		target := rest[:sp]
		body := strings.TrimSpace(rest[sp+1:])
		if body == "" {
			return m.prefillInput("/reply " + target + " ")
		}
		// Route through sendBangReply so the packet actually hits
		// the pump and picks up Data.reply_id threading to the
		// parent message. Body stays clean — no "→<target>: " chrome
		// in the wire payload; the threading line above the row
		// (rendered from replyID) is how "this replies to X" is
		// surfaced to readers.
		//
		// Prefer m.replyParent (captured by `r` in nav mode against
		// the actually-highlighted row) over replyTargetFor's most-
		// recent-from-sender fallback, so threading anchors to the
		// EXACT message the user navigated to — even when the same
		// callsign has several messages in the log.
		parent := m.replyParent
		if parent == 0 {
			parent = m.replyTargetFor(target)
		}
		m.replyParent = 0
		// Plain chat with a Data.reply_id — NOT a /bang command. The
		// renderer reads msg.Bang to decide between yellow `*` and
		// magenta `›` flag glyphs; we want `›` here so a reply
		// looks like the regular outbound chat it actually is.
		m.sendPlainReply(body, parent)
		m.flash = fmt.Sprintf("reply sent to %s", target)
	case "msg":
		// /msg <peer> <text> — real direct message. On the wire it's
		// a TEXT_MESSAGE_APP unicast (MeshPacket.to=peer.NodeNum
		// instead of 0xFFFFFFFF), the same path the official
		// Meshtastic clients use for "Direct Messages." Meshtastic
		// has NO separate port for DMs — broadcast vs DM is purely
		// the To field on the wire. The leading "@" is stripped so
		// `/msg @SGV_Shredder hi` works the same as
		// `/msg SGV_Shredder hi` — the @ is tab-strip chrome, not
		// part of the callsign.
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			m.flash = "usage: /msg <peer> <text>"
			return nil
		}
		target := strings.TrimPrefix(rest[:sp], "@")
		body := strings.TrimSpace(rest[sp+1:])
		if body == "" {
			m.flash = "usage: /msg <peer> <text>"
			return nil
		}
		nodeNum := m.nodeNumOf(target)
		if nodeNum == 0 {
			m.flash = fmt.Sprintf("/msg: peer %q not found in nodes — try /nodes to list", target)
			return nil
		}
		// Resolve the canonical callsign for the flash — accepts
		// shortname or hex prefix on input but always echoes the
		// longname back so the user sees what got sent.
		call := target
		if idx, ok := m.NodesByNum[nodeNum]; ok && idx < len(m.Nodes) {
			call = m.Nodes[idx].Callsign
		}
		m.sendDM(nodeNum, call, body, 0)
		// Switch into the DM thread so subsequent typing routes to
		// this peer without retyping `/msg <peer>` every line.
		// Mirrors irssi's `/msg target text` (focus follows query).
		m.switchToDMThread(nodeNum)
	case "query":
		// /query <peer> opens (or focuses) a DM tab for peer without
		// sending anything yet. Subsequent typing routes through
		// sendPlainMessage → sendDM. Mirrors irssi's /query. Leading
		// "@" is stripped so the user can type what they see in the
		// tab strip ("@SGV_Shredder") verbatim.
		target := strings.TrimPrefix(rest, "@")
		if target == "" {
			m.flash = "usage: /query <peer>"
			return nil
		}
		nodeNum := m.nodeNumOf(target)
		if nodeNum == 0 {
			m.flash = fmt.Sprintf("/query: peer %q not found in nodes — try /nodes to list", target)
			return nil
		}
		call := target
		if idx, ok := m.NodesByNum[nodeNum]; ok && idx < len(m.Nodes) {
			call = m.Nodes[idx].Callsign
		}
		m.switchToDMThread(nodeNum)
		m.flash = fmt.Sprintf("DM thread @%s", call)
	case "close", "unquery":
		// /close (alias /unquery) drops the active DM tab and lands
		// the user back on whichever channel they were on. No-op on a
		// channel tab — channels live on the radio, not in the TUI's
		// tab list. Mirrors irssi's /close + /unquery.
		if m.currentDMNum == 0 {
			m.flash = "/close: not on a DM tab"
			return nil
		}
		closed := m.closeCurrentDMThread()
		m.snapSelectionToTail()
		if closed != "" {
			m.flash = fmt.Sprintf("closed @%s — back to %s", closed, m.CurrentChannel)
		}
	case "join":
		if rest == "" {
			m.flash = "usage: /join <channel>"
			return nil
		}
		// Join by matching name; if not found, flash.
		for i, c := range m.Channels {
			if c.Name == rest || strings.TrimPrefix(c.Name, "#") == rest {
				m.switchChannelByIndex(i)
				return nil
			}
		}
		m.flash = fmt.Sprintf("no channel named %s — /channel list", rest)
	case "part":
		// Meshtastic channels aren't IRC channels — they live on the
		// RADIO as "Channel" config slots (each with a name + a shared
		// PSK), not as a per-client membership. There's nothing for
		// meshX to "part" from; removing a channel means deleting the
		// slot on the radio (phone app or `meshtastic --ch-disable
		// <idx>`). Surface the explanation as a systemBlock instead of
		// a one-line flash so the user sees the model spelled out.
		m.systemBlock(
			"/part",
			"Meshtastic channels live on the radio, not the client.",
			"To leave a channel, disable the slot via the phone app or",
			"the meshtastic CLI (`meshtastic --ch-disable <index>`).",
			"meshX will stop seeing it once the radio drops the slot.",
		)
		m.flash = "/part: channels are radio-configured — see the log"
	case "channels", "list":
		// /list is the IRC convention for "show me the channels."
		m.openOverlay(overlayChannels)
	case "nodes", "who":
		// /who is the IRC convention for "show me the user list" —
		// alias for /nodes so muscle memory from IRC clients lands
		// where users expect. "Node" is the canonical Meshtastic
		// term (we dropped /users + /names).
		m.openOverlay(overlayNodes)
	case "nearby":
		// Distance-sorted roster of peers with a GPS fix — "who
		// can I talk to directly" at a glance. The renderer handles
		// the no-self-fix case with an in-pane explainer so the
		// overlay always opens; refusing here with a transient
		// flash made the command look broken when the user's own
		// radio hadn't broadcast a Position packet yet.
		m.openOverlay(overlayNearby)
	case "radar":
		// Polar scope. Same in-pane explainer as /nearby for the
		// no-self-fix case — always open, show why if data is
		// missing.
		m.openOverlay(overlayRadar)
	case "channel":
		if rest == "list" || rest == "" {
			m.openOverlay(overlayChannels)
			return nil
		}
		// /channel add <url>
		// Accept either a meshtastic://e/#... deep link or an
		// https://meshtastic.org/e/#... universal link. The fragment
		// after `#` is a base64-url ChannelSet protobuf — see
		// channel_url.go for the codec. PSK never touches the network;
		// the URL is a portable PSK envelope, not a server call.
		sub := rest
		arg := ""
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			sub = rest[:i]
			arg = strings.TrimSpace(rest[i+1:])
		}
		switch sub {
		case "add":
			if arg == "" {
				m.flash = "usage: /channel add <meshtastic://url>"
				return nil
			}
			return m.channelAdd(arg)
		case "del", "delete", "rm":
			if arg == "" {
				m.flash = "usage: /channel del <name>"
				return nil
			}
			return m.channelDel(arg)
		case "new":
			if arg == "" {
				m.flash = "usage: /channel new <name>"
				return nil
			}
			return m.channelNew(arg)
		case "share":
			if arg == "" {
				m.flash = "usage: /channel share <name>"
				return nil
			}
			return m.channelShare(arg)
		default:
			m.flash = "usage: /channel list | add <url> | new <name> | share <name> | del <name>"
		}
	case "nick":
		// /nick (no args) — read-only display of the current
		// longname. /nick <name> — immediate write of User.long_name
		// via AdminMessage.SetOwner. No reboot (firmware accepts
		// the write hot); change propagates to peers on the next
		// NodeInfo broadcast. The canonical edit path for both
		// longname and shortname (with draft + Ctrl+S) is /config;
		// /nick stays as the fast inline rename ham operators
		// expect to do without leaving their composing surface.
		// Shortname is round-tripped from the current value so
		// this only changes longname.
		if rest == "" {
			cur := m.myCallsign()
			short := m.myShortName()
			if short != "" {
				m.flash = fmt.Sprintf("nick: %s [%s]  (use /nick <name> to change)", cur, short)
			} else {
				m.flash = fmt.Sprintf("nick: %s  (use /nick <name> to change)", cur)
			}
			return nil
		}
		return m.setOwner(rest, m.myShortName(), "long")
	case "config":
		// Open the radio-config overlay. The interactive panel
		// (configPane) shows an "Radio buzzer: on/off" row at the
		// top — Enter toggles it via AdminMessage.SetModuleConfig.
		// The dump-as-systemBlock variant /config used to do is
		// gone; /info already covers "what does meshx know" and
		// /config is now the consistent path for radio-side knobs.
		m.openOverlay(overlayConfig)
	case "dingtest":
		// Manual BEL verification — returns the exact tea.Cmd
		// applyTextMessage uses on inbound chat. Going through the
		// bubbletea runtime (instead of writing to stdout inline) is
		// what makes the BEL actually reach the terminal under the
		// alt-screen renderer. If the bell still doesn't fire after
		// /dingtest, the cause is your terminal's audible + visual
		// bell preferences (Terminal.app / iTerm Profile → Audible
		// Bell + Visual Bell) — not a meshX bug.
		m.systemBlock(
			"/dingtest",
			"emit:    BEL queued via tea.Cmd",
			"hint:    if no audible/visual bell, check",
			"         Terminal/iTerm Profile → Audible Bell + Visual Bell.",
		)
		m.flash = "/dingtest: BEL queued"
		return ringTerminalBellCmd()
	case "qrtest":
		// Hidden diagnostic — same renderer /channel share uses, so
		// you can iterate on QR layout (quiet zone, half-block math,
		// scanability under your terminal's font / cell aspect)
		// without minting real channels. With no arg, encodes a plain
		// text string that scans to inert text in any QR app — phones
		// see the text and do NOT offer to add a channel. With an
		// arg, encodes that string verbatim so you can smoke-test
		// arbitrary payloads (real meshtastic:// URLs, longer text).
		// Like /dingtest, intentionally NOT in /help — debug surface
		// only.
		payload := rest
		if payload == "" {
			// Plain text on purpose — NOT a meshtastic://e/#... URL.
			// The phone scanner will display this text and do
			// nothing else; we don't want a render-check command to
			// trick the recipient's Meshtastic app into prompting
			// "add channel qrtest?" with a fake PSK. To smoke-test
			// the full share path, run /qrtest <a real URL> instead.
			payload = "meshx /qrtest — render check, scans to plain text only"
		}
		qr, err := renderQRASCII(payload)
		if err != nil {
			m.flash = fmt.Sprintf("/qrtest: render failed: %v", err)
			return nil
		}
		qrLines := strings.Split(qr, "\n")
		lines := make([]string, 0, len(qrLines)+4)
		lines = append(
			lines,
			fmt.Sprintf("payload: %s", payload),
			fmt.Sprintf("size:    %d bytes", len(payload)),
			"note:    scans to plain text — does NOT add a channel",
			"",
		)
		lines = append(lines, qrLines...)
		m.systemBlock("/qrtest", lines...)
		m.flash = "/qrtest: QR rendered — visible in log"
	case "mute":
		// Toggle the meshX terminal ding (BEL on inbound text).
		// Persists to settings.ding_muted so the pref survives
		// restarts. Does NOT touch the radio's onboard buzzer —
		// that's /config → "Radio buzzer". Two separate knobs by
		// design: the radio beeps in your pocket / on your desk,
		// meshX dings inside the terminal.
		m.DingMuted = !m.DingMuted
		v := "off"
		if m.DingMuted {
			v = "on"
		}
		// ding_muted is a meshx-CLIENT preference (terminal beep), not
		// a per-radio knob — pass "" for radioID so it lives once
		// globally rather than once per radio in the settings table.
		m.session.PutSetting("", "ding_muted", v)
		if m.DingMuted {
			m.flash = "/mute on — terminal ding silenced"
			m.systemLine("ding muted — terminal won't beep on incoming text")
		} else {
			m.flash = "/mute off — terminal ding restored"
			m.systemLine("ding unmuted — terminal will beep on incoming text")
		}
	case "me":
		// IRC ASCII-action convention. /me waves → broadcasts the
		// literal "* waves" as a TEXT_MESSAGE_APP packet on the
		// current channel. Routes through sendPlainMessage (NOT
		// sendBang) so msg.Bang stays empty — chatRowRender's
		// action detection requires that to render the row as
		// "* <nick> <action>" in italic, instead of the bang flag
		// /cq, /73, etc. produce. Wire format is just "* <action>"
		// so non-meshx peers see something readable too.
		if rest == "" {
			m.flash = "usage: /me <action>"
			return nil
		}
		m.sendPlainMessage("* " + rest)
		m.flash = fmt.Sprintf("* %s %s", m.myCallsign(), rest)
	case "version":
		// Surface meshX version + radio firmware in one shot. Useful
		// for support tickets, "is my firmware current?" checks, and
		// just-curious. Reads runtime/debug.ReadBuildInfo() so the
		// VCS revision is always accurate without a manual version
		// constant to bump.
		m.systemBlock("/version", buildVersionLines(m)...)
	case "ignore":
		// Local-only filter — hide chat messages from a peer in the
		// messages pane. Doesn't touch the wire (the radio still
		// receives the packets), doesn't persist (in-memory set,
		// cleared on restart). Distinct from nav-m mute which is
		// just a state-marker on the nodes pane. Use /unignore to
		// drop the filter.
		target := rest
		if target == "" {
			m.flash = "usage: /ignore <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("ignore: no node matches %s", target)
			return nil
		}
		if m.Ignored == nil {
			m.Ignored = make(map[string]bool)
		}
		m.Ignored[strings.ToLower(n.Callsign)] = true
		m.flash = fmt.Sprintf("ignoring %s — messages hidden until /unignore", n.Callsign)
		m.systemLine(fmt.Sprintf("ignore: %s — chat messages will be hidden", n.Callsign))
	case "unignore":
		target := rest
		if target == "" {
			if len(m.Ignored) == 0 {
				m.flash = "/unignore: nothing on the ignore list"
				return nil
			}
			calls := make([]string, 0, len(m.Ignored))
			for k := range m.Ignored {
				calls = append(calls, k)
			}
			m.flash = "currently ignoring: " + strings.Join(
				calls,
				", ",
			) + "  (use /unignore <call>)"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("unignore: no node matches %s", target)
			return nil
		}
		key := strings.ToLower(n.Callsign)
		if !m.Ignored[key] {
			m.flash = fmt.Sprintf("unignore: %s wasn't on the list", n.Callsign)
			return nil
		}
		delete(m.Ignored, key)
		m.flash = fmt.Sprintf("unignoring %s — messages will show again", n.Callsign)
		m.systemLine(fmt.Sprintf("unignore: %s — chat messages restored", n.Callsign))
	case "reboot":
		// Sends AdminMessage_RebootSeconds(5) to our own radio.
		if m.session.PumpHandle() == nil {
			m.flash = "/reboot: needs a live radio connection"
			return nil
		}
		res, err := m.session.Reboot(radio.RebootRequest{})
		if err != nil {
			m.flash = fmt.Sprintf("/reboot: %v", err)
			return nil
		}
		m.flash = fmt.Sprintf(
			"/reboot: radio will restart in %ds — meshx will reconnect automatically",
			res.Seconds,
		)
		m.systemLine(
			fmt.Sprintf("reboot: AdminMessage sent — radio restarting in %ds", res.Seconds),
		)
	case "info", "whoami":
		// /info — dump meshx's current knowledge to the log so you
		// can diagnose "why don't I have a name for this peer?"
		// without external tooling. Shows our own identity, a
		// peer-count breakdown (real names vs. unresolved "node 0x…"
		// placeholders), session state, and channel summary.
		lines := []string{
			fmt.Sprintf(
				"self:     %s (0x%x)  shortname=%s",
				m.myCallsign(),
				m.MyNodeNum,
				m.myShortName(),
			),
		}
		if n := m.myNode(); n != nil {
			lines = append(lines, fmt.Sprintf("hw:       %s  fw=%s", n.HwModel, m.RadioFirmware))
		}
		var resolved, ghosts int
		for _, n := range m.Nodes {
			if strings.HasPrefix(n.Callsign, "node 0x") {
				ghosts++
			} else {
				resolved++
			}
		}
		lines = append(
			lines,
			fmt.Sprintf(
				"peers:    %d total  (%d named, %d placeholder)",
				len(m.Nodes),
				resolved,
				ghosts,
			),
			fmt.Sprintf("channels: %d", len(m.Channels)),
			fmt.Sprintf("connected: %t  handshake_complete=%t", m.Connected, m.Connected),
		)
		if m.RadioRegion != "" {
			lines = append(lines, fmt.Sprintf("region:   %s  preset=%s  tx=%d dBm  role=%s",
				m.RadioRegion, m.RadioModemPreset, m.RadioTxPower, m.RadioRole))
		}
		if ghosts > 0 {
			const maxList = 10
			header := fmt.Sprintf("unresolved peers (%d):", ghosts)
			if ghosts > maxList {
				header = fmt.Sprintf("unresolved peers (first %d of %d):", maxList, ghosts)
			}
			lines = append(lines, header)
			n := 0
			for _, node := range m.Nodes {
				if strings.HasPrefix(node.Callsign, "node 0x") {
					lines = append(lines, "  "+node.Callsign)
					n++
					if n >= maxList {
						break
					}
				}
			}
			lines = append(lines, "(try /sync to re-request the radio's NodeDB)")
		}
		m.systemBlock("info", lines...)
	case "sync":
		// /sync — ask the radio to re-dump its config + NodeDB via a
		// fresh WantConfigId handshake. Use when you suspect the
		// cache is stale, or after the radio just resolved a peer
		// you want surfaced without waiting for the next organic
		// NODEINFO_APP broadcast.
		if m.session.PumpHandle() == nil {
			m.flash = "/sync needs a live radio connection (demo mode)"
			return nil
		}
		if _, err := m.session.Sync(); err != nil {
			m.flash = fmt.Sprintf("/sync: %v", err)
			return nil
		}
		// Snapshot current ghost count so we can report the delta
		// when the matching ConfigComplete lands.
		ghosts := 0
		for _, n := range m.Nodes {
			if strings.HasPrefix(n.Callsign, "node 0x") {
				ghosts++
			}
		}
		// Store as non-zero sentinel even when zero ghosts exist, so
		// the ConfigComplete handler can tell a pending /sync apart
		// from the startup handshake.
		if ghosts == 0 {
			m.SyncPendingGhosts = -1
		} else {
			m.SyncPendingGhosts = ghosts
		}
		m.systemBlock(
			"sync",
			"requested NodeDB re-dump",
			fmt.Sprintf("baseline: %d unresolved peers", ghosts),
			"watching for incoming NodeInfo — any placeholder that resolves",
			"will fire its own `identified` line; summary lands on completion.",
		)
	case "help", "h":
		// /help             → open the full scrollable overlay
		// /help <verb>      → irssi / BitchX-style per-command usage
		//                     + summary card dropped inline as a
		//                     systemBlock so it lives in the log
		//                     alongside the exchange it's helping
		//                     with (no modal context switch).
		verb := strings.ToLower(strings.TrimSpace(rest))
		if verb == "" {
			m.mode = modeHelp
			return nil
		}
		verb = strings.TrimPrefix(verb, "/")
		entry, ok := helpEntries[verb]
		if !ok {
			m.flash = fmt.Sprintf("no help for /%s — try /help alone for the full index", verb)
			return nil
		}
		m.systemBlock(
			fmt.Sprintf("help /%s", verb),
			"usage:   "+entry.usage,
			"summary: "+entry.summary,
		)
	case "lastlog":
		// /lastlog              — jump to the very last message
		// /lastlog <call|text>  — jump to the last chat message FROM
		//                          <call> (matches the from column,
		//                          not body), or the last row whose
		//                          body contains <text> if no sender
		//                          matches. Substring + case-
		//                          insensitive lookup, same loose
		//                          match /whois uses.
		// Closes any overlay, lands in nav mode on the located row.
		if len(m.Messages) == 0 {
			m.flash = "/lastlog: log is empty"
			return nil
		}
		m.overlay = overlayNone
		m.focused = paneMessages
		m.input.Blur()
		idx := -1
		if rest == "" {
			idx = len(m.Messages) - 1
		} else {
			needle := strings.ToLower(strings.TrimSpace(rest))
			// First pass: prefer matches in the from column — that's
			// what "the last message FROM gleep" means semantically.
			for i := len(m.Messages) - 1; i >= 0; i-- {
				if m.Messages[i].Status == mdl.StatusSystem {
					continue
				}
				if strings.Contains(strings.ToLower(m.Messages[i].From), needle) {
					idx = i
					break
				}
			}
			// Second pass: body match if no sender hit. Lets users
			// /lastlog "morning" find the last message containing it.
			if idx < 0 {
				for i := len(m.Messages) - 1; i >= 0; i-- {
					if m.Messages[i].Status == mdl.StatusSystem {
						continue
					}
					if strings.Contains(strings.ToLower(m.Messages[i].Text), needle) {
						idx = i
						break
					}
				}
			}
		}
		if idx < 0 {
			m.flash = fmt.Sprintf("lastlog: no chat row matches %q", rest)
			m.mode = modeNav
			return nil
		}
		m.selectedMsg = idx
		m.mode = modeNav
		hit := m.Messages[idx]
		m.flash = fmt.Sprintf("lastlog: %s — %s", hit.Time, hit.From)
	case "search":
		if rest == "" {
			// Toggle behavior — clear an active query, otherwise
			// hint at the syntax. "/search" with nothing was the
			// only way to drop a stale query without going through
			// nav-mode `/` then Esc; bind it here so the muscle-
			// memory "type /search to manage search" works both
			// directions.
			if m.searchQuery != "" {
				m.searchQuery = ""
				m.flash = "search cleared"
				return nil
			}
			m.flash = "usage: /search <pattern>  (press / in nav for live-filter)"
			return nil
		}
		m.searchQuery = strings.ToLower(rest)
		// Walk backward from the current selection — chat logs read
		// newest-first, so /search should land on the MOST RECENT
		// match (just-above-the-cursor or near-the-tail), not the
		// oldest one. n/N still step in their bound directions, so
		// once we're on the most-recent hit n walks further back
		// through history and N walks forward toward newer matches.
		if ok, count := m.jumpToSearchHit(-1); ok {
			m.flash = fmt.Sprintf(
				"search: %d matches for %q — n/N to step, /search to clear",
				count,
				rest,
			)
			m.mode = modeNav
			m.input.Blur()
		} else {
			m.flash = fmt.Sprintf("no match for %q", rest)
			m.searchQuery = ""
		}
	case "clear":
		m.Messages = nil
		m.selectedMsg = 0
		m.flash = "scrollback cleared"

	default:
		m.flash = fmt.Sprintf("unknown /%s — see /help", verb)
	}
	return nil
}

// sendBang appends an outgoing command-originated message to the
// local log AND (in live-radio mode) transmits it over LoRa via the
// pump. The `bang` field is kept purely for local UI styling — the
// on-wire text is just `body`, clean enough that any other
// Meshtastic client reads it as plain chat.
//
// Used by /cq, /73, /qsl, /qth, /grid, /rs, /cqr, /sk, /qrz, /qrm,
// /qsb, /wx, /k, /mesh. Commands that don't transmit (/whois, /ping,
// /tr, /env, /config) use systemLine() instead.
func (m *model) sendBang(bang, body string) {
	m.sendBangReply(bang, body, 0)
}

// sendBangReply is sendBang with an optional reply target — when
// replyToID is non-zero, the outgoing packet carries Data.reply_id
// pointing at the parent message, and the local log entry records
// the same replyID so the renderer can draw a quoted-parent line
// above the reply.
func (m *model) sendBangReply(bang, body string, replyToID uint32) {
	channel := int(m.currentChannelIndex())
	pid, _ := m.session.Send(mdl.SendText{
		Channel: channel,
		Text:    body,
		ReplyID: replyToID,
	})
	res := m.session.RecordOutbound(radio.RecordOutboundOptions{
		Channel:  channel,
		Text:     body,
		Bang:     bang,
		ReplyID:  replyToID,
		PacketID: pid,
	})
	if res.Index >= 0 {
		m.selectedMsg = res.Index
	}
	m.focused = paneMessages
}

// replyTargetFor returns the packetID of the most recent message
// from the given callsign, or 0 if none exists. Used by directed
// ham verbs (/73 <call>, /qsl <call>, /sk <call>, /rs <call>, etc.)
// to thread the outgoing reply to whatever <call> most recently
// said — the Meshtastic "reply to" semantic.
func (m *model) replyTargetFor(call string) uint32 {
	if call == "" {
		return 0
	}
	target := strings.ToLower(strings.TrimSpace(call))
	for i := len(m.Messages) - 1; i >= 0; i-- {
		msg := m.Messages[i]
		if msg.Mine || msg.Status == mdl.StatusSystem || msg.PacketID == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(msg.From), target) {
			return msg.PacketID
		}
	}
	return 0
}

// updateHelp handles keys while the help overlay is visible. Vim-style
// scroll: j/k lines, d/u half-page, g/G top/bottom, q / ? / Enter /
// ESC dismiss. Ctrl+X still exits the whole app.
