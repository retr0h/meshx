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

// config.go — modeled radio config types. Today only
// ExternalNotification (the buzzer module) lives here because that's
// the one config the /config UI lets the user round-trip; future
// configs that need full-record persistence (Owner, LoRa,
// DeviceConfig, Position, …) get their typed projections here too,
// each backed by proto<->model bridges in pump/config.go.

// ExternalNotification is the flat Go projection of the Meshtastic
// firmware's `ExternalNotificationConfig` proto. Every field a
// recent firmware ships in the message lives here — the model
// captures the whole record so toggling one user-visible setting
// (e.g. AlertMessageBuzzer) preserves everything else verbatim on
// save. Earlier code stashed the raw `*pb.ModuleConfig_External
// NotificationConfig` snapshot to get the same round-trip property;
// pulling the proto out of the meshx package lets the future
// `meshx serve` daemon's HTTP/JSON contract not inherit the proto's
// shape.
//
// The proto<->model bridges live in package pump (the only place
// that imports gomeshproto). Consumers see only this struct.
//
// New firmware fields land here additively — keeping a wider
// snapshot than older firmware populates is safe because the
// pump's FromProto translator zeros what the proto omits, and
// ToProto drops fields the firmware doesn't read.
type ExternalNotification struct {
	// Enabled is the master switch — false silences the module
	// regardless of every other field below.
	Enabled bool

	// OutputMs is the buzzer/vibra duration, in milliseconds, per
	// notification fire. 0 means "use firmware default."
	OutputMs uint32

	// Output is the GPIO pin number the firmware drives.
	Output uint32

	// OutputVibra / OutputBuzzer are alternate GPIOs for split-pin
	// hardware (vibration motor on one, buzzer on another).
	OutputVibra  uint32
	OutputBuzzer uint32

	// Active inverts the GPIO polarity (false = active-low).
	Active bool

	// AlertMessage / AlertMessageVibra / AlertMessageBuzzer fan out
	// the "new chat message" trigger across the three output kinds.
	AlertMessage       bool
	AlertMessageVibra  bool
	AlertMessageBuzzer bool

	// AlertBell / AlertBellVibra / AlertBellBuzzer mirror the same
	// three for the "bell" notification class (the firmware's
	// secondary alert tier — used by the sender-pressed "ring my
	// bell" meta).
	AlertBell       bool
	AlertBellVibra  bool
	AlertBellBuzzer bool

	// UseI2SAsBuzzer routes the buzzer through the I2S audio bus on
	// boards that support it.
	UseI2SAsBuzzer bool

	// Nag is the repeat interval (seconds) the firmware re-fires
	// the alert at. 0 disables nagging.
	Nag uint32

	// UsePwm tells the firmware to drive the buzzer pin with PWM
	// instead of straight on/off — quieter, less startling.
	UsePwm bool
}
