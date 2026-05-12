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

import mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

// Tool registration hub. tools_gen.go (produced by mcpgen from
// api.yaml) owns every tool; this file just calls the generated
// entry point. When you add a new HTTP endpoint to the daemon,
// run `just generate` and the MCP tool appears automatically —
// no hand-wiring.

func (s *Server) registerTools() {
	s.registerGeneratedTools()
	s.registerEventTools()
}

// textResult wraps a string as an MCP CallToolResult with a single
// TextContent block — the canonical response shape for every
// JSON-returning tool the generator emits.
func textResult(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: text},
		},
	}
}

// jsonOrErr renders a value as pretty JSON for an MCP TextContent
// response, or returns an mcp-typed error string if marshaling
// fails.
func jsonOrErr(v any) string {
	b, err := jsonMarshalIndent(v)
	if err != nil {
		return "error: marshal response: " + err.Error()
	}
	return string(b)
}
