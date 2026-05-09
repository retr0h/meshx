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

package sdk

import (
	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/sdk/gen"
	"github.com/retr0h/meshx/internal/session"
)

// convert.go is the SDK boundary — gen.* are wire shapes (whatever
// the OpenAPI spec produced via codegen), mdl.* are domain shapes.
// Width-of-int and pointer-vs-value mismatches collapse here so the
// rest of the codebase only sees the domain types. External SDK
// consumers (OpenCLAW, third-party clients) follow the same pattern
// in their language: gen → their domain.
//
// Keep the converters dumb — straight field copies. Anything fancier
// (validation, defaulting, derived fields) belongs upstream on the
// daemon side, not here.

func channelFromGen(g gen.ChannelItem) mdl.ChannelItem {
	return mdl.ChannelItem{
		Name:    g.Name,
		Private: g.Private,
		Unread:  int(g.Unread),
		Index:   int(g.Index),
		Role:    g.Role,
		HasPSK:  g.HasPsk,
	}
}

func nodeFromGen(g gen.NodeItem) mdl.NodeItem {
	return mdl.NodeItem{
		Callsign:    g.Callsign,
		ShortName:   g.ShortName,
		NodeNum:     uint32(g.NodeNum),
		Unresolved:  g.Unresolved,
		State:       mdl.NodeState(g.State),
		Fav:         g.Fav,
		LastHeard:   g.LastHeard,
		LastHeardAt: g.LastHeardAt,
		HeardRank:   int(g.HeardRank),
		LastSNR:     g.LastSnr,
		LastRSSI:    g.LastRssi,
		LastHops:    int(g.LastHops),
		HwModel:     g.HwModel,
		Firmware:    g.Firmware,
	}
}

func messageFromGen(g gen.MessageItem) mdl.MessageItem {
	m := mdl.MessageItem{
		Message: mdl.Message{
			Time:     g.Time,
			From:     g.From,
			Text:     g.Text,
			Mine:     g.Mine,
			Status:   mdl.MessageStatus(g.Status),
			Hops:     int(g.Hops),
			PacketID: uint32(g.PacketId),
			FromNum:  uint32(g.FromNum),
			ToNum:    uint32(g.ToNum),
			SentAt:   g.SentAt,
		},
	}
	if g.Snr != nil {
		m.SNR = *g.Snr
	}
	if g.ReplyId != nil {
		m.ReplyID = uint32(*g.ReplyId)
	}
	if g.Corrupted != nil {
		m.Corrupted = *g.Corrupted
	}
	if g.Ackers != nil {
		m.Ackers = make([]mdl.Acker, 0, len(*g.Ackers))
		for _, a := range *g.Ackers {
			m.Ackers = append(m.Ackers, mdl.Acker{
				NodeNum:  uint32(a.NodeNum),
				Callsign: a.Callsign,
				Hops:     int(a.Hops),
				At:       a.At,
			})
		}
	}
	// Bang is a TUI render hint (json:"-" on Message) and does not
	// ride the API wire; remote clients re-derive it from the
	// leading word of Text if they want ham-bang detection.
	// Group, ExpireAt, and Pinned are similarly render-only fields
	// tagged json:"-". The remote side recomputes them from local
	// TUI state on receipt.
	return m
}

// applySessionSnapshot copies a /radios/{id} response into State.
// Only writes fields the snapshot owns — leaves Channels/Nodes/
// Messages alone since those are populated from their own GET
// endpoints.
func applySessionSnapshot(st *session.State, g gen.SessionSnapshot) {
	st.RadioID = g.RadioId
	st.MyNodeNum = uint32(g.MyNodeNum)
	st.Connected = g.Connected
	st.CurrentChannel = g.CurrentChannel
	st.ConnectDest = g.ConnectDest
	if g.RadioFirmware != nil {
		st.RadioFirmware = *g.RadioFirmware
	}
	if g.RadioRegion != nil {
		st.RadioRegion = *g.RadioRegion
	}
	if g.RadioModem != nil {
		st.RadioModemPreset = *g.RadioModem
	}
	if g.RadioRole != nil {
		st.RadioRole = *g.RadioRole
	}
	if g.BatteryLevel != nil {
		st.BatteryLevel = uint32(*g.BatteryLevel)
	}
	if g.BatteryVoltage != nil {
		st.BatteryVoltage = *g.BatteryVoltage
	}
	if g.ChannelUtil != nil {
		st.ChannelUtil = *g.ChannelUtil
	}
	if g.AirUtilTx != nil {
		st.AirUtilTx = *g.AirUtilTx
	}
	if g.MyLatitude != nil {
		st.MyLatitude = *g.MyLatitude
	}
	if g.MyLongitude != nil {
		st.MyLongitude = *g.MyLongitude
	}
	if g.MyAltitude != nil {
		st.MyAltitude = *g.MyAltitude
	}
	if g.MyGrid != nil {
		st.MyGrid = *g.MyGrid
	}
}
