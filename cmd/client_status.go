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
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// clientStatusCmd hits /healthz then /radios so the operator can
// confirm the daemon is reachable and see every attached radio in
// one go. Useful before `meshx client connect` to pick which radio
// to target when more than one is registered.
var clientStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print daemon health + every attached radio",
	Long: `GETs /healthz and /radios from the configured daemon. Prints
"ok" + a table of (RADIO_ID, MY_NODE_NUM, CONNECTED, CONNECT_DEST)
per radio. Non-zero exit on connection failure or non-2xx.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		logger.With(slog.String("subsystem", "client.status")).
			Debug("running", slog.String("server", cfg.ServerURL))
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientStatus(context.Background(), c, os.Stdout)
	},
}

// runClientStatus is the testable core — takes a wired SDK client +
// a writer so httptest harnesses can verify the output without going
// through cobra/viper.
func runClientStatus(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
) error {
	hResp, err := c.HealthWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("client status: GET /healthz: %w", err)
	}
	if hResp.JSON200 == nil {
		return fmt.Errorf(
			"client status: GET /healthz returned %s",
			hResp.Status(),
		)
	}
	_, _ = fmt.Fprintf(w, "daemon: %s\n\n", hResp.JSON200.Status)

	rResp, err := c.ListRadiosWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("client status: GET /radios: %w", err)
	}
	if rResp.JSON200 == nil {
		return fmt.Errorf(
			"client status: GET /radios returned %s",
			rResp.Status(),
		)
	}
	if rResp.JSON200.Radios == nil || len(*rResp.JSON200.Radios) == 0 {
		_, _ = fmt.Fprintln(w, "no radios attached.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RADIO_ID\tMY_NODE_NUM\tCONNECTED\tCONNECT_DEST")
	for _, r := range *rResp.JSON200.Radios {
		connected := "no"
		if r.Connected {
			connected = "yes"
		}
		_, _ = fmt.Fprintf(
			tw, "%s\t0x%x\t%s\t%s\n",
			r.RadioId, r.MyNodeNum, connected, orDash(r.ConnectDest),
		)
	}
	return tw.Flush()
}
