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

package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

var clientForgetCmd = &cobra.Command{
	Use:   "forget <uuid|name>",
	Short: "Remove a paired BLE device from the daemon's store",
	Long: `DELETEs /transports/ble/devices/{uuid}. The daemon resolves either
a UUID or a human-readable name (longname) against its store, so
either works. 404 when no saved device matches.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		needle := args[0]
		logger.With(slog.String("subsystem", "client.forget")).
			Debug(
				"running",
				slog.String("server", cfg.ServerURL),
				slog.String("needle", needle),
			)
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientForget(context.Background(), c, os.Stdout, needle)
	},
}

func runClientForget(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
	needle string,
) error {
	resp, err := c.ForgetBleDeviceWithResponse(ctx, needle)
	if err != nil {
		return fmt.Errorf("client forget: %w", err)
	}
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("client forget: daemon returned %s", resp.Status())
	}
	_, _ = fmt.Fprintf(w, "forgot %s\n", needle)
	return nil
}
