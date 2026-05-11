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
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// tailSinceCursor is the optional resume cursor for `meshx client
// tail`. When non-zero, sent as `?since=N` so the daemon replays
// ring-buffered events with id > N before streaming new ones. The
// SSE handler also honors `Last-Event-ID:` headers but `?since` is
// easier from a shell.
var tailSinceCursor string

var clientTailCmd = &cobra.Command{
	Use:   "tail <radio_id>",
	Short: "Stream radio events as JSON lines (SSE consumer)",
	Long: `Opens a streaming GET against /radios/{radio_id}/events. Each
inbound event surfaces as one line of JSON (event kind + payload),
suitable for piping into ` + "`jq`" + ` or appending to a log.

  meshx client tail 0xabcdef01
  meshx client tail 0xabcdef01 --since 1024
  meshx client tail 0xabcdef01 | jq 'select(.kind == "text")'

oapi-codegen doesn't emit a typed client for SSE so the request is
hand-rolled; auth + the daemon URL still come from the inherited
--server / --auth-token-file flags.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		radioID := args[0]
		logger.With(slog.String("subsystem", "client.tail")).
			Debug(
				"running",
				slog.String("server", cfg.ServerURL),
				slog.String("radio_id", radioID),
				slog.String("since", tailSinceCursor),
			)
		return runClientTail(context.Background(), cfg, os.Stdout, radioID, tailSinceCursor)
	},
}

func init() {
	clientTailCmd.Flags().StringVar(
		&tailSinceCursor,
		"since",
		"",
		"resume cursor — replay events with id > N from the ring buffer before streaming new ones",
	)
}

// runClientTail issues the SSE request and prints one
// `{"kind":"…","data":…}` JSON line per inbound event. Returns when
// ctx cancels or the upstream closes; the daemon's SSE handler is
// long-lived, so this typically only returns on Ctrl-C.
func runClientTail(
	ctx context.Context,
	cfg clientConfig,
	w io.Writer,
	radioID, since string,
) error {
	url := strings.TrimRight(cfg.ServerURL, "/") + "/radios/" + radioID + "/events"
	if since != "" {
		url += "?since=" + since
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("client tail: build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("client tail: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("client tail: daemon returned %s", resp.Status)
	}

	rd := bufio.NewReader(resp.Body)
	var (
		kind string
		eid  string
		data strings.Builder
	)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("client tail: read: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			// End of event — emit a JSON envelope and reset.
			if kind != "" {
				body := strings.TrimSpace(data.String())
				if eid != "" {
					_, _ = fmt.Fprintf(
						w, "{\"id\":%q,\"kind\":%q,\"data\":%s}\n",
						eid, kind, body,
					)
				} else {
					_, _ = fmt.Fprintf(
						w, "{\"kind\":%q,\"data\":%s}\n",
						kind, body,
					)
				}
			}
			kind = ""
			eid = ""
			data.Reset()
		case strings.HasPrefix(line, "id:"):
			eid = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			kind = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line, "data:"))
		}
		// Comment lines (": …") and unknown prefixes fall through
		// the switch and are silently dropped, matching the SSE spec.
	}
}
