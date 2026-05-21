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

package tui

import (
	"strings"
	"testing"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

func TestMessageRowVisualHeight(t *testing.T) {
	tests := []struct {
		name string
		msg  messageItem
		want int
	}{
		{
			name: "single-line-message",
			msg:  messageItem{Message: mdl.Message{Text: "hello world"}},
			want: 1,
		},
		{
			name: "two-line-message",
			msg:  messageItem{Message: mdl.Message{Text: "line one\nline two"}},
			want: 2,
		},
		{
			name: "three-line-message",
			msg:  messageItem{Message: mdl.Message{Text: "a\nb\nc"}},
			want: 3,
		},
		{
			name: "message-with-ackers-adds-one",
			msg: messageItem{
				Message: mdl.Message{Text: "hello"},
				Ackers:  []mdl.Acker{{Callsign: "KC7ABC"}},
			},
			want: 2,
		},
		{
			name: "multi-line-with-ackers",
			msg: messageItem{
				Message: mdl.Message{Text: "line one\nline two"},
				Ackers:  []mdl.Acker{{Callsign: "KC7ABC"}},
			},
			want: 3,
		},
		{
			name: "reply-with-no-parent-in-messages-no-extra-height",
			msg: messageItem{
				Message: mdl.Message{Text: "reply", ReplyID: 0x9999},
			},
			// ReplyID set but no parent in m.Messages => no threading quote
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			if got := messageRowVisualHeight(m, tt.msg); got != tt.want {
				t.Errorf("messageRowVisualHeight() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMessageRowVisualHeightWithReplyParent(t *testing.T) {
	// When the parent IS in m.Messages the height gets +1 for the threading quote.
	parent := messageItem{Message: mdl.Message{Text: "parent msg", PacketID: 0x1234}}
	reply := messageItem{Message: mdl.Message{Text: "reply msg", ReplyID: 0x1234}}

	m := newTestModel()
	m.Messages = []messageItem{parent, reply}

	got := messageRowVisualHeight(m, reply)
	if got != 2 {
		t.Errorf("messageRowVisualHeight(reply with parent) = %d, want 2", got)
	}
}

func TestMessageRowRenderEmptyBox(t *testing.T) {
	// Empty box must return empty string — no panic.
	r := messageRow{
		m:   newTestModel(),
		msg: messageItem{Message: mdl.Message{Text: "hello"}},
	}
	got := r.Render(Box{Width: 0, Height: 0})
	if got != "" {
		t.Errorf("messageRow.Render(empty box) = %q, want empty string", got)
	}
}

func TestMessageRowRenderLineCount(t *testing.T) {
	tests := []struct {
		name      string
		msg       messageItem
		boxHeight int
		wantLines int
	}{
		{
			name:      "single-line-chat",
			msg:       messageItem{Message: mdl.Message{Text: "hello world", From: "KC7ABC"}},
			boxHeight: 1,
			wantLines: 1,
		},
		{
			name:      "box-height-two-pads-to-two-lines",
			msg:       messageItem{Message: mdl.Message{Text: "hello", From: "KC7ABC"}},
			boxHeight: 2,
			wantLines: 2,
		},
		{
			name: "notice-row-single-line",
			msg: messageItem{
				Message: mdl.Message{
					Text:   "-!- storage degraded",
					Status: mdl.StatusNotice,
				},
			},
			boxHeight: 1,
			wantLines: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := messageRow{
				m:         newTestModel(),
				msg:       tt.msg,
				rowBg:     rowBgOdd,
				rowsInner: 80,
			}
			got := r.Render(Box{Width: 80, Height: tt.boxHeight})
			lines := strings.Split(got, "\n")
			if len(lines) != tt.wantLines {
				t.Errorf(
					"messageRow.Render() produced %d lines, want %d",
					len(lines), tt.wantLines,
				)
			}
		})
	}
}
