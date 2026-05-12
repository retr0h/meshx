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

package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// Config + reboot — User-record patch (longname / shortname /
// is_licensed) and the buzzer toggle live in one update_config tool
// (PATCH semantics: only supplied fields are dispatched).

func (s *Server) registerConfigTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "update_config",
		Description: "Patch the radio's User record (longname / shortname / is_licensed) and the external-notification buzzer. Only the fields you set are dispatched — omitted ones are preserved. Use empty strings or null to mean 'not supplied' (the underlying AdminMessage is partial-update aware).",
	}, s.toolUpdateConfig)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "reboot_radio",
		Description: "Schedule a firmware reboot. seconds=0 (default) uses the 5s grace window; seconds=N waits N seconds before resetting. The daemon stays up; its pump reconnects automatically when the radio comes back.",
	}, s.toolRebootRadio)
}

type updateConfigArgs struct {
	RadioID    string `json:"radio_id"              jsonschema:"canonical radio identifier from list_radios"`
	LongName   string `json:"longname,omitempty"    jsonschema:"radio operator longname (1..36 bytes UTF-8); omit to preserve"`
	ShortName  string `json:"shortname,omitempty"   jsonschema:"radio operator shortname / tag (1..4 bytes UTF-8; emoji counts as its byte length); omit to preserve"`
	IsLicensed *bool  `json:"is_licensed,omitempty" jsonschema:"FCC-licensed flag on the User record; omit to preserve"`
	Buzzer     *bool  `json:"buzzer,omitempty"      jsonschema:"toggle the radio's external-notification buzzer (true = on, false = off); omit to preserve"`
}

func (s *Server) toolUpdateConfig(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args updateConfigArgs,
) (*mcpsdk.CallToolResult, any, error) {
	body := gen.UpdateConfigJSONRequestBody{}
	if args.LongName != "" {
		v := args.LongName
		body.Longname = &v
	}
	if args.ShortName != "" {
		v := args.ShortName
		body.Shortname = &v
	}
	if args.IsLicensed != nil {
		body.IsLicensed = args.IsLicensed
	}
	if args.Buzzer != nil {
		body.Buzzer = args.Buzzer
	}
	resp, err := s.client.UpdateConfigWithResponse(ctx, args.RadioID, body)
	if err != nil {
		return nil, nil, fmt.Errorf("update_config: %w", err)
	}
	if resp.JSON202 == nil {
		return nil, nil, fmt.Errorf("update_config: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON202)), nil, nil
}

type rebootRadioArgs struct {
	RadioID string `json:"radio_id"          jsonschema:"canonical radio identifier from list_radios"`
	Seconds int    `json:"seconds,omitempty" jsonschema:"delay before reboot in seconds; 0 (default) = 5s grace window"`
}

func (s *Server) toolRebootRadio(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args rebootRadioArgs,
) (*mcpsdk.CallToolResult, any, error) {
	body := gen.RebootRadioJSONRequestBody{}
	if args.Seconds > 0 {
		v := int32(args.Seconds)
		body.Seconds = &v
	}
	resp, err := s.client.RebootRadioWithResponse(ctx, args.RadioID, body)
	if err != nil {
		return nil, nil, fmt.Errorf("reboot_radio: %w", err)
	}
	if resp.JSON202 == nil {
		return nil, nil, fmt.Errorf("reboot_radio: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON202)), nil, nil
}
