// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
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
	"sort"
)

// GET /radios + GET /radios/{radio_id} — registry projection.
// Surfaces every attached *radio.Session as a flat summary so clients
// can enumerate radios + drill into one for telemetry without having
// to subscribe to /events.

// RadioSummary is one entry in GET /radios.
//
// MyNodeNum is uint32 on the wire (Meshtastic's chipset-derived id,
// can exceed int32 max). Tagged format:"int64" minimum:"0" so the
// OpenAPI spec emits a wide-enough integer type — without these the
// generated SDK narrows to int32 and overflows on real-world radios.
// Same pattern wherever a uint32 surfaces in the HTTP wire shape.
type RadioSummary struct {
	RadioID     string `json:"radio_id"     doc:"canonical radio identifier — 0x<hex node_num> post-handshake, pending:<transport>:<addr> beforehand"`
	MyNodeNum   uint32 `json:"my_node_num"  doc:"radio's own node num; zero before MyInfo arrives"                                                    format:"int64" minimum:"0"`
	Connected   bool   `json:"connected"    doc:"true once ConfigComplete has fired"`
	ConnectDest string `json:"connect_dest" doc:"transport target string (/dev/cu.*, host:port, ble:<uuid>)"`
}

// SessionSnapshot is the GET /radios/{radio_id} response.
type SessionSnapshot struct {
	RadioID        string  `json:"radio_id"`
	MyNodeNum      uint32  `json:"my_node_num"               format:"int64" minimum:"0"`
	Connected      bool    `json:"connected"`
	CurrentChannel string  `json:"current_channel"`
	ConnectDest    string  `json:"connect_dest"`
	RadioFirmware  string  `json:"radio_firmware,omitempty"`
	RadioRegion    string  `json:"radio_region,omitempty"`
	RadioModem     string  `json:"radio_modem,omitempty"`
	RadioRole      string  `json:"radio_role,omitempty"`
	BatteryLevel   uint32  `json:"battery_level,omitempty"   format:"int64" minimum:"0"`
	BatteryVoltage float32 `json:"battery_voltage,omitempty"`
	ChannelUtil    float32 `json:"channel_util,omitempty"`
	AirUtilTx      float32 `json:"air_util_tx,omitempty"`
	MyLatitude     float64 `json:"my_latitude,omitempty"`
	MyLongitude    float64 `json:"my_longitude,omitempty"`
	MyAltitude     int32   `json:"my_altitude,omitempty"`
	MyGrid         string  `json:"my_grid,omitempty"`
}

type listRadiosInput struct{}

type listRadiosOutput struct {
	Body struct {
		Radios []RadioSummary `json:"radios"`
	}
}

func (s *Server) handleListRadios(
	_ context.Context,
	_ *listRadiosInput,
) (*listRadiosOutput, error) {
	out := &listRadiosOutput{}
	out.Body.Radios = []RadioSummary{}
	if s.radios == nil {
		return out, nil
	}
	ids := s.radios.IDs()
	sort.Strings(ids)
	for _, id := range ids {
		d, ok := s.radios.Get(id)
		if !ok {
			continue
		}
		st := d.Snapshot()
		if st == nil {
			out.Body.Radios = append(out.Body.Radios, RadioSummary{RadioID: id})
			continue
		}
		out.Body.Radios = append(out.Body.Radios, RadioSummary{
			RadioID:     id,
			MyNodeNum:   st.MyNodeNum,
			Connected:   st.Connected,
			ConnectDest: st.ConnectDest,
		})
	}
	return out, nil
}

type getRadioInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
}

type getRadioOutput struct {
	Body SessionSnapshot
}

func (s *Server) handleGetRadio(_ context.Context, in *getRadioInput) (*getRadioOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	out := &getRadioOutput{}
	st := d.Snapshot()
	if st == nil {
		return out, nil
	}
	out.Body = SessionSnapshot{
		RadioID:        st.RadioID,
		MyNodeNum:      st.MyNodeNum,
		Connected:      st.Connected,
		CurrentChannel: st.CurrentChannel,
		ConnectDest:    st.ConnectDest,
		RadioFirmware:  st.RadioFirmware,
		RadioRegion:    st.RadioRegion,
		RadioModem:     st.RadioModemPreset,
		RadioRole:      st.RadioRole,
		BatteryLevel:   st.BatteryLevel,
		BatteryVoltage: st.BatteryVoltage,
		ChannelUtil:    st.ChannelUtil,
		AirUtilTx:      st.AirUtilTx,
		MyLatitude:     st.MyLatitude,
		MyLongitude:    st.MyLongitude,
		MyAltitude:     st.MyAltitude,
		MyGrid:         st.MyGrid,
	}
	return out, nil
}
