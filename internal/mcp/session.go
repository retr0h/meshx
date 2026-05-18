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

package mcp

import (
	"context"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// Driver is the narrow consumer-surface the MCP server requires of
// its SDK client, declared at the consumer seam per the osapi-io
// pattern. Concrete *gen.ClientWithResponses satisfies it
// structurally — the compiler verifies at the assignment site in
// New().
//
// This mirrors the shape internal/server/session.go and
// internal/tui/session.go take: every package that consumes a
// "driver" (the daemon, the TUI, this MCP server) declares a narrow
// interface for what it actually uses. Concrete-type imports stay on
// the side that constructs the driver (New()), and the rest of the
// package depends only on this interface. Lets a test slot in a
// fake without dragging the SDK's generated client into scope.
//
// New methods get added here first (declare what we need) then the
// concrete *gen.ClientWithResponses already satisfies them.
type Driver interface {
	// Radio enumeration + telemetry reads.
	ListRadiosWithResponse(
		ctx context.Context,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ListRadiosResponse, error)
	GetRadioWithResponse(
		ctx context.Context,
		radioID string,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.GetRadioResponse, error)

	// Channel / node / message reads.
	ListChannelsWithResponse(
		ctx context.Context,
		radioID string,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ListChannelsResponse, error)
	ListNodesWithResponse(
		ctx context.Context,
		radioID string,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ListNodesResponse, error)
	ListMessagesWithResponse(
		ctx context.Context,
		radioID string,
		params *gen.ListMessagesParams,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ListMessagesResponse, error)

	// Message dispatch.
	SendMessageWithResponse(
		ctx context.Context,
		radioID string,
		params *gen.SendMessageParams,
		body gen.SendMessageJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.SendMessageResponse, error)

	// Channel mutations — mint / import / delete / share.
	MintChannelWithResponse(
		ctx context.Context,
		radioID string,
		body gen.MintChannelJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.MintChannelResponse, error)
	ImportChannelsWithResponse(
		ctx context.Context,
		radioID string,
		body gen.ImportChannelsJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ImportChannelsResponse, error)
	DeleteChannelWithResponse(
		ctx context.Context,
		radioID string,
		index int64,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.DeleteChannelResponse, error)
	ShareChannelWithResponse(
		ctx context.Context,
		radioID string,
		index int64,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ShareChannelResponse, error)

	// Config + radio-op dispatches.
	UpdateConfigWithResponse(
		ctx context.Context,
		radioID string,
		body gen.UpdateConfigJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.UpdateConfigResponse, error)
	RebootRadioWithResponse(
		ctx context.Context,
		radioID string,
		body gen.RebootRadioJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.RebootRadioResponse, error)
	SyncRadioWithResponse(
		ctx context.Context,
		radioID string,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.SyncRadioResponse, error)
	PingPeerWithResponse(
		ctx context.Context,
		radioID string,
		body gen.PingPeerJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.PingPeerResponse, error)
	TraceroutePeerWithResponse(
		ctx context.Context,
		radioID string,
		body gen.TraceroutePeerJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.TraceroutePeerResponse, error)

	// Radio lifecycle.
	AttachRadioWithResponse(
		ctx context.Context,
		body gen.AttachRadioJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.AttachRadioResponse, error)
	DetachRadioWithResponse(
		ctx context.Context,
		radioID string,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.DetachRadioResponse, error)

	// Meta.
	HealthWithResponse(
		ctx context.Context,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.HealthResponse, error)

	// Transport admin.
	AutoDetectUsbWithResponse(
		ctx context.Context,
		body gen.AutoDetectUsbJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.AutoDetectUsbResponse, error)
	ListBleDevicesWithResponse(
		ctx context.Context,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ListBleDevicesResponse, error)
	ScanBleWithResponse(
		ctx context.Context,
		body gen.ScanBleJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ScanBleResponse, error)
	ScanUsbWithResponse(
		ctx context.Context,
		body gen.ScanUsbJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ScanUsbResponse, error)
	PairBleWithResponse(
		ctx context.Context,
		body gen.PairBleJSONRequestBody,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.PairBleResponse, error)
	ForgetBleDeviceWithResponse(
		ctx context.Context,
		uuid string,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ForgetBleDeviceResponse, error)
	SetBleFavoriteWithResponse(
		ctx context.Context,
		uuid string,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.SetBleFavoriteResponse, error)
	ClearBleFavoriteWithResponse(
		ctx context.Context,
		reqEditors ...gen.RequestEditorFn,
	) (*gen.ClearBleFavoriteResponse, error)
}
