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

package pump

// config_test.go — tests for ExternalNotificationFromProto and
// ExternalNotificationToProto. Both are pure projection functions (no I/O).

import (
	"testing"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// ---- ExternalNotificationFromProto ------------------------------------------

func TestExternalNotificationFromProto_NilReturnsZero(t *testing.T) {
	got := ExternalNotificationFromProto(nil)
	if got != (model.ExternalNotification{}) {
		t.Errorf("expected zero ExternalNotification, got %+v", got)
	}
}

func TestExternalNotificationFromProto_AllFields(t *testing.T) {
	ext := &pb.ModuleConfig_ExternalNotificationConfig{
		Enabled:            true,
		OutputMs:           250,
		Output:             17,
		OutputVibra:        18,
		OutputBuzzer:       19,
		Active:             true,
		AlertMessage:       true,
		AlertMessageVibra:  true,
		AlertMessageBuzzer: true,
		AlertBell:          true,
		AlertBellVibra:     true,
		AlertBellBuzzer:    true,
		UseI2SAsBuzzer:     true,
		NagTimeout:         60,
		UsePwm:             true,
	}
	got := ExternalNotificationFromProto(ext)

	if !got.Enabled {
		t.Error("Enabled: want true")
	}
	if got.OutputMs != 250 {
		t.Errorf("OutputMs: got %d", got.OutputMs)
	}
	if got.Output != 17 {
		t.Errorf("Output: got %d", got.Output)
	}
	if got.OutputVibra != 18 {
		t.Errorf("OutputVibra: got %d", got.OutputVibra)
	}
	if got.OutputBuzzer != 19 {
		t.Errorf("OutputBuzzer: got %d", got.OutputBuzzer)
	}
	if !got.Active {
		t.Error("Active: want true")
	}
	if !got.AlertMessage {
		t.Error("AlertMessage: want true")
	}
	if !got.AlertMessageVibra {
		t.Error("AlertMessageVibra: want true")
	}
	if !got.AlertMessageBuzzer {
		t.Error("AlertMessageBuzzer: want true")
	}
	if !got.AlertBell {
		t.Error("AlertBell: want true")
	}
	if !got.AlertBellVibra {
		t.Error("AlertBellVibra: want true")
	}
	if !got.AlertBellBuzzer {
		t.Error("AlertBellBuzzer: want true")
	}
	if !got.UseI2SAsBuzzer {
		t.Error("UseI2SAsBuzzer: want true")
	}
	if got.Nag != 60 {
		t.Errorf("Nag: got %d", got.Nag)
	}
	if !got.UsePwm {
		t.Error("UsePwm: want true")
	}
}

func TestExternalNotificationFromProto_ZeroProto(t *testing.T) {
	ext := &pb.ModuleConfig_ExternalNotificationConfig{}
	got := ExternalNotificationFromProto(ext)
	// All bool fields default false, uint32 default 0.
	if got.Enabled || got.AlertMessageBuzzer || got.UsePwm {
		t.Errorf("expected all-false for zero proto, got %+v", got)
	}
	if got.OutputMs != 0 || got.Output != 0 {
		t.Errorf("expected zero numeric fields, got %+v", got)
	}
}

// ---- ExternalNotificationToProto --------------------------------------------

func TestExternalNotificationToProto_AllFields(t *testing.T) {
	m := model.ExternalNotification{
		Enabled:            true,
		OutputMs:           500,
		Output:             21,
		OutputVibra:        22,
		OutputBuzzer:       23,
		Active:             true,
		AlertMessage:       true,
		AlertMessageVibra:  false,
		AlertMessageBuzzer: true,
		AlertBell:          false,
		AlertBellVibra:     false,
		AlertBellBuzzer:    true,
		UseI2SAsBuzzer:     false,
		Nag:                30,
		UsePwm:             true,
	}
	got := ExternalNotificationToProto(m)

	if !got.GetEnabled() {
		t.Error("Enabled: want true")
	}
	if got.GetOutputMs() != 500 {
		t.Errorf("OutputMs: got %d", got.GetOutputMs())
	}
	if got.GetOutput() != 21 {
		t.Errorf("Output: got %d", got.GetOutput())
	}
	if got.GetOutputVibra() != 22 {
		t.Errorf("OutputVibra: got %d", got.GetOutputVibra())
	}
	if got.GetOutputBuzzer() != 23 {
		t.Errorf("OutputBuzzer: got %d", got.GetOutputBuzzer())
	}
	if !got.GetActive() {
		t.Error("Active: want true")
	}
	if !got.GetAlertMessage() {
		t.Error("AlertMessage: want true")
	}
	if got.GetAlertMessageVibra() {
		t.Error("AlertMessageVibra: want false")
	}
	if !got.GetAlertMessageBuzzer() {
		t.Error("AlertMessageBuzzer: want true")
	}
	if !got.GetAlertBellBuzzer() {
		t.Error("AlertBellBuzzer: want true")
	}
	if got.GetNagTimeout() != 30 {
		t.Errorf("NagTimeout: got %d", got.GetNagTimeout())
	}
	if !got.GetUsePwm() {
		t.Error("UsePwm: want true")
	}
}

// ---- Round-trip: FromProto ∘ ToProto = identity ----------------------------

func TestExternalNotification_RoundTrip(t *testing.T) {
	original := model.ExternalNotification{
		Enabled:            true,
		OutputMs:           150,
		Output:             7,
		OutputVibra:        8,
		OutputBuzzer:       9,
		Active:             false,
		AlertMessage:       true,
		AlertMessageVibra:  false,
		AlertMessageBuzzer: true,
		AlertBell:          false,
		AlertBellVibra:     true,
		AlertBellBuzzer:    false,
		UseI2SAsBuzzer:     false,
		Nag:                120,
		UsePwm:             true,
	}

	proto := ExternalNotificationToProto(original)
	restored := ExternalNotificationFromProto(proto)

	if restored != original {
		t.Errorf("round-trip mismatch:\n  original: %+v\n  restored: %+v", original, restored)
	}
}
