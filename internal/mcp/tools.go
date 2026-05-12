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

// Tool registration hub. Each tool_*.go file owns one operation
// family (matching the internal/server/handlers_*.go split) and
// exports a register<Family>Tools(s) function that wires that
// family's tools onto s.mcp. registerTools below is the single
// entry point New() calls.

func (s *Server) registerTools() {
	s.registerRadioTools()
	s.registerChannelTools()
	s.registerMessageTools()
	s.registerConfigTools()
	s.registerRadioOpTools()
	s.registerTransportTools()
}

// jsonOrErr renders a value as pretty JSON for an MCP TextContent
// response, or returns an mcp-typed error string if marshaling
// fails. Used by read-shaped tools whose canonical output is the
// daemon's JSON response — preserves field names so the agent sees
// the same shape `curl … | jq` would yield.
func jsonOrErr(v any) string {
	b, err := jsonMarshalIndent(v)
	if err != nil {
		return "error: marshal response: " + err.Error()
	}
	return string(b)
}
