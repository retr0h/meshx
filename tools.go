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

//go:build tools

// Package tools is a build-tag-gated anchor for library deps that are
// only imported by codegen sources (//go:build ignore main.go files
// under internal/tui/emoji and internal/sdk/gen). Without this file,
// `go mod tidy` strips those deps from go.mod because no normal-build
// code reaches them; then `go generate` fails with "go.mod needs
// tidy" when it tries to compile the build-ignored main.go.
//
// The `//go:build tools` tag means this file is never part of a real
// compilation — go tooling sees the imports for module-graph purposes
// only. The standard pattern; predates the `tool` directive (Go 1.24+)
// which only handles executable-tool main packages, not library
// imports like jennifer.
//
// Add a blank import here whenever a new build-ignored generator
// imports a library that isn't already pulled in by normal code.

package tools

import (
	// jennifer/jen — typed Go-source AST builder used by
	// internal/tui/emoji/main.go to emit widths.gen.go.
	_ "github.com/dave/jennifer/jen"
)
