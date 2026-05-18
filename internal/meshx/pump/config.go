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

import (
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// ExternalNotificationFromProto projects the firmware's
// ExternalNotificationConfig protobuf into the flat model struct.
// Used by translate() when a FromRadio_ModuleConfig (or AdminMessage
// GetModuleConfigResponse) lands. Returns the zero ExternalNotification
// when ext is nil — a "buzzer-toggle saved before the live config
// arrived" still gets a usable round-trip target.
func ExternalNotificationFromProto(
	ext *pb.ModuleConfig_ExternalNotificationConfig,
) model.ExternalNotification {
	if ext == nil {
		return model.ExternalNotification{}
	}
	return model.ExternalNotification{
		Enabled:            ext.GetEnabled(),
		OutputMs:           ext.GetOutputMs(),
		Output:             ext.GetOutput(),
		OutputVibra:        ext.GetOutputVibra(),
		OutputBuzzer:       ext.GetOutputBuzzer(),
		Active:             ext.GetActive(),
		AlertMessage:       ext.GetAlertMessage(),
		AlertMessageVibra:  ext.GetAlertMessageVibra(),
		AlertMessageBuzzer: ext.GetAlertMessageBuzzer(),
		AlertBell:          ext.GetAlertBell(),
		AlertBellVibra:     ext.GetAlertBellVibra(),
		AlertBellBuzzer:    ext.GetAlertBellBuzzer(),
		UseI2SAsBuzzer:     ext.GetUseI2SAsBuzzer(),
		Nag:                ext.GetNagTimeout(),
		UsePwm:             ext.GetUsePwm(),
	}
}

// ExternalNotificationToProto is the inverse — used by the meshx
// package's outbound config-write path so commands.go never has to
// construct an ExternalNotificationConfig directly. Mirrors the
// FromProto projection field-for-field.
func ExternalNotificationToProto(
	m model.ExternalNotification,
) *pb.ModuleConfig_ExternalNotificationConfig {
	return &pb.ModuleConfig_ExternalNotificationConfig{
		Enabled:            m.Enabled,
		OutputMs:           m.OutputMs,
		Output:             m.Output,
		OutputVibra:        m.OutputVibra,
		OutputBuzzer:       m.OutputBuzzer,
		Active:             m.Active,
		AlertMessage:       m.AlertMessage,
		AlertMessageVibra:  m.AlertMessageVibra,
		AlertMessageBuzzer: m.AlertMessageBuzzer,
		AlertBell:          m.AlertBell,
		AlertBellVibra:     m.AlertBellVibra,
		AlertBellBuzzer:    m.AlertBellBuzzer,
		UseI2SAsBuzzer:     m.UseI2SAsBuzzer,
		NagTimeout:         m.Nag,
		UsePwm:             m.UsePwm,
	}
}
