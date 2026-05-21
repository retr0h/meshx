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
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

func TestBuildNotice(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		style     noticeStyle
		wantText  string
		wantStyle *noticeStyle
	}{
		{
			name:     "default-style-adds-prefix",
			text:     "storage degraded",
			style:    noticeStyle{},
			wantText: "-!- storage degraded",
		},
		{
			name:  "custom-style-preserved",
			text:  "splash art row",
			style: noticeStyle{fg: "#ff0000", bold: true, center: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := buildNotice(tt.text, tt.style)
			if row.Status != mdl.StatusNotice {
				t.Errorf("buildNotice() Status = %v, want StatusNotice", row.Status)
			}
			if !strings.Contains(row.Text, "-!- ") {
				t.Errorf("buildNotice() Text = %q, want '-!- ' prefix", row.Text)
			}
			if tt.wantText != "" && row.Text != tt.wantText {
				t.Errorf("buildNotice() Text = %q, want %q", row.Text, tt.wantText)
			}
			if row.Style == nil {
				t.Error("buildNotice() Style is nil, want non-nil")
			}
		})
	}
}

func TestNoticeFadeAlpha(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		msg       messageItem
		now       time.Time
		wantZero  bool
		wantOne   bool
		wantRange bool // 0 < alpha < 1
	}{
		{
			name:     "permanent-row-is-always-zero",
			msg:      messageItem{Message: mdl.Message{Text: "permanent"}},
			now:      now,
			wantZero: true,
		},
		{
			name: "pinned-row-is-zero",
			msg: func() messageItem {
				exp := now.Add(5 * time.Second)
				return messageItem{
					Message:  mdl.Message{Text: "pinned"},
					ExpireAt: &exp,
					Pinned:   true,
				}
			}(),
			now:      now,
			wantZero: true,
		},
		{
			name: "expired-row-is-one",
			msg: func() messageItem {
				past := now.Add(-1 * time.Second)
				return messageItem{
					Message:  mdl.Message{Text: "expired"},
					ExpireAt: &past,
				}
			}(),
			now:     now,
			wantOne: true,
		},
		{
			name: "fresh-row-far-from-expiry-is-zero",
			msg: func() messageItem {
				future := now.Add(60 * time.Second)
				return messageItem{
					Message:  mdl.Message{Text: "fresh"},
					ExpireAt: &future,
				}
			}(),
			now:      now,
			wantZero: true,
		},
		{
			name: "row-inside-fade-window-is-between-zero-and-one",
			msg: func() messageItem {
				// 5s left = middle of the 10s fade window
				soonExpiry := now.Add(5 * time.Second)
				return messageItem{
					Message:  mdl.Message{Text: "fading"},
					ExpireAt: &soonExpiry,
				}
			}(),
			now:       now,
			wantRange: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := noticeFadeAlpha(tt.msg, tt.now)
			switch {
			case tt.wantZero && got != 0:
				t.Errorf("noticeFadeAlpha() = %v, want 0", got)
			case tt.wantOne && got != 1:
				t.Errorf("noticeFadeAlpha() = %v, want 1", got)
			case tt.wantRange && (got <= 0 || got >= 1):
				t.Errorf("noticeFadeAlpha() = %v, want (0, 1)", got)
			}
		})
	}
}

func TestLerpHex(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		t    float64
		want string
	}{
		{name: "t=0-returns-a", a: "#ff0000", b: "#0000ff", t: 0, want: "#ff0000"},
		{name: "t=1-returns-b", a: "#ff0000", b: "#0000ff", t: 1, want: "#0000ff"},
		{
			name: "t=0.5-midpoint",
			a:    "#000000",
			b:    "#ffffff",
			t:    0.5,
			want: "#7f7f7f",
		},
		{
			name: "negative-t-clamps-to-a",
			a:    "#ff0000",
			b:    "#0000ff",
			t:    -0.5,
			want: "#ff0000",
		},
		{
			name: "t-greater-than-1-clamps-to-b",
			a:    "#ff0000",
			b:    "#0000ff",
			t:    1.5,
			want: "#0000ff",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lerpHex(tt.a, tt.b, tt.t); got != tt.want {
				t.Errorf("lerpHex(%q, %q, %v) = %q, want %q", tt.a, tt.b, tt.t, got, tt.want)
			}
		})
	}
}

func TestHexToRGB(t *testing.T) {
	tests := []struct {
		name    string
		hex     string
		r, g, b int
	}{
		{name: "black", hex: "#000000", r: 0, g: 0, b: 0},
		{name: "white", hex: "#ffffff", r: 255, g: 255, b: 255},
		{name: "red", hex: "#ff0000", r: 255, g: 0, b: 0},
		{name: "green", hex: "#00ff00", r: 0, g: 255, b: 0},
		{name: "blue", hex: "#0000ff", r: 0, g: 0, b: 255},
		{name: "without-hash", hex: "ff0000", r: 255, g: 0, b: 0},
		{name: "invalid-returns-zero", hex: "bad", r: 0, g: 0, b: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b := hexToRGB(tt.hex)
			if r != tt.r || g != tt.g || b != tt.b {
				t.Errorf("hexToRGB(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.hex, r, g, b, tt.r, tt.g, tt.b)
			}
		})
	}
}

func TestNextGroupID(t *testing.T) {
	// nextGroupID must be monotonically increasing and non-zero.
	a := nextGroupID()
	b := nextGroupID()
	if b <= a {
		t.Errorf("nextGroupID not monotonically increasing: %d then %d", a, b)
	}
	if a == 0 {
		t.Error("nextGroupID returned 0")
	}
}

func TestLastEphemeralNoticeIdx(t *testing.T) {
	tests := []struct {
		name     string
		messages []messageItem
		want     int
	}{
		{
			name:     "no-messages",
			messages: nil,
			want:     -1,
		},
		{
			name: "no-expirable-messages",
			messages: []messageItem{
				{Message: mdl.Message{Text: "permanent"}},
			},
			want: -1,
		},
		{
			name: "last-expirable-returned",
			messages: func() []messageItem {
				exp := time.Now().Add(30 * time.Second)
				return []messageItem{
					{Message: mdl.Message{Text: "a"}},
					{Message: mdl.Message{Text: "b"}, ExpireAt: &exp},
					{Message: mdl.Message{Text: "c"}},
				}
			}(),
			want: 1,
		},
		{
			name: "returns-last-of-multiple-expirable",
			messages: func() []messageItem {
				exp1 := time.Now().Add(10 * time.Second)
				exp2 := time.Now().Add(20 * time.Second)
				return []messageItem{
					{Message: mdl.Message{Text: "a"}, ExpireAt: &exp1},
					{Message: mdl.Message{Text: "b"}, ExpireAt: &exp2},
				}
			}(),
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Messages = tt.messages
			if got := m.lastEphemeralNoticeIdx(); got != tt.want {
				t.Errorf("lastEphemeralNoticeIdx() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReapExpiredNotices(t *testing.T) {
	now := time.Now()
	past := now.Add(-2 * time.Second)
	future := now.Add(60 * time.Second)

	tests := []struct {
		name         string
		messages     []messageItem
		wantCount    int
		wantTexts    []string
		wantNotTexts []string
	}{
		{
			name:      "empty-messages",
			messages:  nil,
			wantCount: 0,
		},
		{
			name: "expired-single-row-removed",
			messages: []messageItem{
				{Message: mdl.Message{Text: "expired"}, ExpireAt: &past},
				{Message: mdl.Message{Text: "fresh"}, ExpireAt: &future},
			},
			wantCount:    1,
			wantTexts:    []string{"fresh"},
			wantNotTexts: []string{"expired"},
		},
		{
			name: "permanent-row-kept",
			messages: []messageItem{
				{Message: mdl.Message{Text: "permanent"}},
				{Message: mdl.Message{Text: "expired"}, ExpireAt: &past},
			},
			wantCount:    1,
			wantTexts:    []string{"permanent"},
			wantNotTexts: []string{"expired"},
		},
		{
			name: "pinned-expired-row-kept",
			messages: []messageItem{
				{Message: mdl.Message{Text: "pinned-expired"}, ExpireAt: &past, Pinned: true},
			},
			wantCount: 1,
			wantTexts: []string{"pinned-expired"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Messages = tt.messages
			m.reapExpiredNotices()
			if len(m.Messages) != tt.wantCount {
				t.Errorf("after reap: len(Messages) = %d, want %d", len(m.Messages), tt.wantCount)
			}
			for _, want := range tt.wantTexts {
				found := false
				for _, msg := range m.Messages {
					if msg.Text == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("after reap: expected to find message %q but didn't", want)
				}
			}
			for _, notWant := range tt.wantNotTexts {
				for _, msg := range m.Messages {
					if msg.Text == notWant {
						t.Errorf("after reap: did not expect message %q but found it", notWant)
					}
				}
			}
		})
	}
}

func TestToggleNoticePin(t *testing.T) {
	tests := []struct {
		name       string
		setupMsgs  func() []messageItem
		idx        int
		wantPinned bool
		wantNoop   bool
	}{
		{
			name:      "out-of-range-noop",
			setupMsgs: func() []messageItem { return nil },
			idx:       0,
			wantNoop:  true,
		},
		{
			name: "permanent-row-noop",
			setupMsgs: func() []messageItem {
				return []messageItem{
					{Message: mdl.Message{Text: "permanent"}},
				}
			},
			idx:      0,
			wantNoop: true,
		},
		{
			name: "pin-ephemeral-row",
			setupMsgs: func() []messageItem {
				exp := time.Now().Add(30 * time.Second)
				return []messageItem{
					{Message: mdl.Message{Text: "ephemeral"}, ExpireAt: &exp},
				}
			},
			idx:        0,
			wantPinned: true,
		},
		{
			name: "unpin-pinned-row",
			setupMsgs: func() []messageItem {
				exp := time.Now().Add(30 * time.Second)
				return []messageItem{
					{
						Message:         mdl.Message{Text: "was-pinned"},
						ExpireAt:        &exp,
						Pinned:          true,
						PinnedRemaining: 30 * time.Second,
					},
				}
			},
			idx:        0,
			wantPinned: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Messages = tt.setupMsgs()
			if tt.wantNoop {
				// Just verify no panic.
				m.toggleNoticePin(tt.idx)
				return
			}
			m.toggleNoticePin(tt.idx)
			if m.Messages[tt.idx].Pinned != tt.wantPinned {
				t.Errorf(
					"toggleNoticePin(%d): Pinned = %v, want %v",
					tt.idx, m.Messages[tt.idx].Pinned, tt.wantPinned,
				)
			}
		})
	}
}
