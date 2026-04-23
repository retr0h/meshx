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

package meshx

import (
	"fmt"
	"log"
	"strings"
)

// logger.go is the process-level logging seam. Two concerns live
// here:
//
//   1. fatalf — genuinely unrecoverable paths call this to terminate.
//      It mirrors log.Fatalf's os.Exit(1) semantics. Any subsystem
//      (storage, transport, pre-TUI initialization) can import-free
//      call fatalf; the rule is "fatal means the Bubble Tea program
//      hasn't started yet or is already torn down." Inside a running
//      TUI, prefer systemLine / flashf (see notices.go) — log.Fatalf
//      would spew over the rendered screen.
//
//   2. noticesLogger — an adapter that implements the Logger
//      interface (the shape goose / other third-party libraries
//      expect) while routing informational output into an in-memory
//      notes slice the caller surfaces via systemLine. Fatal calls
//      still terminate via fatalf.
//
// Keeping this out of storage.go means any future third-party
// library that wants a log hook (migration tools, RPC clients,
// config readers, etc.) can reuse the same adapter instead of
// rolling its own.

// Logger matches the shape goose.Logger / stdlib log.Logger /
// virtually every third-party library's logger interface. Exposed
// here so our Noticeslogger / any future hook can be type-asserted
// by any library that wants a logger seam.
type Logger interface {
	Print(v ...any)
	Println(v ...any)
	Printf(format string, v ...any)
	Fatal(v ...any)
	Fatalf(format string, v ...any)
}

// noticesLogger is a Logger implementation that captures
// informational output into an in-memory notes slice the caller
// drains via systemLine, and routes Fatal* through the shared
// fatalf so the process terminates cleanly. Used by runMigrations
// to redirect goose's stderr spew into the messages pane without
// ever touching the terminal.
type noticesLogger struct{ notes *[]string }

func (l noticesLogger) Print(v ...any)                 { l.capture(fmt.Sprint(v...)) }
func (l noticesLogger) Println(v ...any)               { l.capture(fmt.Sprint(v...)) }
func (l noticesLogger) Printf(format string, v ...any) { l.capture(fmt.Sprintf(format, v...)) }

// Fatal / Fatalf terminate. Route through the shared fatalf so every
// subsystem uses the same exit path — someday we might want to flip
// that to "drop a crash dump + exit" and keeping the call site
// single means one edit instead of chasing log.Fatal calls across
// the tree.
func (l noticesLogger) Fatal(v ...any)                 { fatalf("%s", fmt.Sprint(v...)) }
func (l noticesLogger) Fatalf(format string, v ...any) { fatalf(format, v...) }

func (l noticesLogger) capture(s string) {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return
	}
	*l.notes = append(*l.notes, s)
}

// fatalf is the single process-exit helper. Prints the formatted
// message to stderr via stdlib log.Fatalf, which os.Exit(1)s after
// the write. Callers inside a running Bubble Tea program should
// NOT use this — m.systemLine / m.flashf keep the UI alive and let
// the user see the failure in context. Only pre-TUI init paths
// (storage migrations, config load, etc.) and genuinely
// unrecoverable library callbacks should fatalf.
func fatalf(format string, v ...any) {
	log.Fatalf(format, v...)
}
