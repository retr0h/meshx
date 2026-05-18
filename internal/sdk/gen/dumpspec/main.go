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

// Command dumpspec extracts the daemon's OpenAPI 3.0 spec directly
// from Huma in-process and writes it to api.yaml — no listener, no
// port allocation, no curl. Invoked via go:generate from the
// sibling gen package's generate.go ahead of the oapi-codegen step,
// so `just generate` runs end-to-end with no background daemon.
//
// Registered as a `tool` in go.mod (same pattern as gofumpt /
// golines / oapi-codegen / emojigen) so internal/server lands in
// the require graph through a real-build subpackage. Invoked via
// `go tool` from the sibling gen/generate.go's go:generate
// directive.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/retr0h/meshx/internal/server"
)

func main() {
	out := flag.String(
		"out",
		"api.yaml",
		"output path for the OpenAPI 3.0 yaml (relative to the directory go:generate runs in — internal/sdk/gen/)",
	)
	flag.Parse()

	s := server.New(server.Config{Radios: server.NewRegistry()})
	yaml, err := s.OpenAPISpec()
	if err != nil {
		log.Fatalf("dumpspec: extract spec: %v", err)
	}
	if err := os.WriteFile(*out, yaml, 0o600); err != nil {
		log.Fatalf("dumpspec: write %s: %v", *out, err)
	}
	abs, _ := filepath.Abs(*out)
	log.Printf("dumpspec: wrote %d bytes to %s", len(yaml), abs)
}
