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

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// GET /radios/{radio_id}/messages — projection of State.Messages with
// optional ?dm= and ?limit= filtering. The dm filter runs before
// limit so a caller asking specifically for DMs doesn't lose them to
// a noisy channel pushing them out of the tail.

type listMessagesInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Limit   int    `                doc:"max rows to return; 0 = no limit"                                                                                                                                   query:"limit" default:"0"`
	DM      string `                doc:"DM filter; '' = all rows, '1' = peer-addressed DMs (either direction), 'mine' = DMs to/from my_node_num. Use to skip channel-firehose filtering on the client side" query:"dm"                enum:",1,mine"`
}

type listMessagesOutput struct {
	Body struct {
		Messages []mdl.MessageItem `json:"messages"`
	}
}

// matchesDMFilter reports whether a row passes the ?dm= query
// filter. "" = no filter (every row). "1" = any peer-addressed DM
// (ToNum is a specific peer, not broadcast / unset). "mine" = DMs
// I'm a participant in (incoming-to-me OR outgoing-by-me to a
// specific peer). Other values are rejected by the input enum so
// this falls through to a permissive default.
func matchesDMFilter(m mdl.MessageItem, mode string, myNodeNum uint32) bool {
	switch mode {
	case "":
		return true
	case "1":
		return m.ToNum != 0 && m.ToNum != mdl.BroadcastNum
	case "mine":
		if myNodeNum != 0 && m.ToNum == myNodeNum {
			return true
		}
		return m.Mine && m.ToNum != 0 && m.ToNum != mdl.BroadcastNum
	default:
		return true
	}
}

func (s *Server) handleListMessages(
	_ context.Context,
	in *listMessagesInput,
) (*listMessagesOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	out := &listMessagesOutput{}
	out.Body.Messages = []mdl.MessageItem{}
	st := d.Snapshot()
	if st == nil {
		return out, nil
	}
	msgs := st.Messages
	if in.DM != "" {
		filtered := make([]mdl.MessageItem, 0, len(msgs))
		for _, m := range msgs {
			if matchesDMFilter(m, in.DM, st.MyNodeNum) {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if in.Limit > 0 && len(msgs) > in.Limit {
		msgs = msgs[len(msgs)-in.Limit:]
	}
	out.Body.Messages = append(out.Body.Messages, msgs...)
	return out, nil
}
