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
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// fav / unfav are split into two top-level verbs (rather than one
// `fav <uuid> --clear`) because the underlying endpoints are
// asymmetric: set is PUT /transports/ble/devices/{uuid}/favorite
// (needs a target), clear is DELETE /transports/ble/favorite (no
// path arg — clears whichever device is currently favorite). Same
// shape as the local `meshx ble fav` + `meshx ble disconnect`
// commands they replace when the daemon owns the adapter.

var clientFavCmd = &cobra.Command{
	Use:   "fav <uuid|name>",
	Short: "Mark a paired BLE device as the bare-launch favorite",
	Long: `PUTs /transports/ble/devices/{uuid}/favorite. Sets the device as
the one ` + "`meshx`" + ` falls through to when no transport arg is
given. The daemon resolves either a UUID or a longname against its
store.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		needle := args[0]
		logger.With(slog.String("subsystem", "client.fav")).
			Debug(
				"running",
				slog.String("server", cfg.ServerURL),
				slog.String("needle", needle),
			)
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientFav(context.Background(), c, os.Stdout, needle)
	},
}

func runClientFav(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
	needle string,
) error {
	resp, err := c.SetBleFavoriteWithResponse(ctx, needle)
	if err != nil {
		return fmt.Errorf("client fav: %w", err)
	}
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("client fav: daemon returned %s", resp.Status())
	}
	_, _ = fmt.Fprintf(w, "favorite set: %s\n", needle)
	return nil
}

var clientUnfavCmd = &cobra.Command{
	Use:   "unfav",
	Short: "Clear the bare-launch BLE favorite",
	Long: `DELETEs /transports/ble/favorite. Clears whichever device is
currently the bare-launch favorite — after this, ` + "`meshx`" + ` won't
auto-connect on bare launch until a new favorite is set.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		logger.With(slog.String("subsystem", "client.unfav")).
			Debug("running", slog.String("server", cfg.ServerURL))
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientUnfav(context.Background(), c, os.Stdout)
	},
}

func runClientUnfav(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
) error {
	resp, err := c.ClearBleFavoriteWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("client unfav: %w", err)
	}
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("client unfav: daemon returned %s", resp.Status())
	}
	_, _ = fmt.Fprintln(w, "favorite cleared")
	return nil
}
