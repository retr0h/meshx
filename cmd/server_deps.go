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

package cmd

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/meshx/storage"
)

// openStore opens the shared sqlite handle (~/.meshx/meshx.db),
// running migrations as needed. Returns nil on failure with a
// structured warning — read-only HTTP routes still serve.
//
// The daemon (`meshx server start`) keeps the handle for the
// process lifetime so the per-radio session can persist messages
// and the /transports/* surface can CRUD pairings. The CLI
// one-shots open + close on every invocation via cliTransports
// instead.
func openStore(_ *cobra.Command, log *slog.Logger) *storage.Sqlite {
	path, err := storage.DefaultPath()
	if err != nil {
		log.Warn("storage disabled: cannot resolve path", slog.Any("error", err))
		return nil
	}
	s, err := storage.New(path)
	if err != nil {
		log.Warn(
			"storage disabled: open failed",
			slog.String("path", path),
			slog.Any("error", err),
		)
		return nil
	}
	log.Info("storage opened", slog.String("path", path))
	return s
}
