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

// Package cmd contains the meshx cobra command tree.
package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"

	"github.com/retr0h/meshx/internal/cli"
)

// logger is the package-level slog logger, populated from initLogger
// after cobra parses persistent flags. CLI subcommands log through it
// directly; the daemon hands a child logger (with subsystem tag) to
// the server so HTTP and CLI lines stay distinguishable in a unified
// stream.
var (
	logger     = slog.New(slog.NewTextHandler(os.Stderr, nil))
	jsonOutput bool
)

var rootCmd = &cobra.Command{
	Use:   "meshx",
	Short: "Glitched-out terminal Meshtastic messenger",
	Long: `meshx is an irssi-style terminal Meshtastic messenger — an
irssi/BitchX/mutt-inspired chat client for your LoRa radio with a
vintage BBS aesthetic.

Pick a transport explicitly:

  meshx usb connect [dev]        # open the TUI over USB serial
  meshx ble connect <uuid|name>  # open the TUI over Bluetooth (paired)
  meshx server start             # run the headless HTTP+SSE daemon`,
	RunE: func(c *cobra.Command, _ []string) error {
		return c.Help()
	},
}

// Execute runs the root command; invoked by main. SilenceUsage drops
// the help-text dump on runtime failures (auto-connect with no
// radios, bind:port in use, …) where it's just noise. Cobra already
// prints "Error: <err>" on its own.
func Execute() {
	rootCmd.SilenceUsage = true

	// Wrap cobra's default help to print the themed banner above it.
	// SetHelpFunc fires for `meshx --help` and for the bare-command
	// fallback alike, so the banner shows in both paths without
	// duplicating itself.
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		if c == rootCmd {
			out := c.OutOrStdout()
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprint(out, cli.Banner(out))
			_, _ = fmt.Fprintln(out)
		}
		defaultHelp(c, args)
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig, initLogger)

	rootCmd.PersistentFlags().BoolP("debug", "d", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVarP(&jsonOutput, "json", "j", false, "emit logs as JSON")

	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
}

// initConfig wires viper — env-var overrides take effect through a
// MESHX_… prefix with dots replaced by underscores, so e.g.
// MESHX_SERVE_BIND=0.0.0.0:9000 overrides the serve.bind default.
// Defaults seeded here are the source of truth; flags binding to the
// same key win over both env and default at runtime.
func initConfig() {
	viper.SetEnvPrefix("meshx")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Server defaults — override via --bind flag, MESHX_SERVER_BIND
	// env, or a future config file. 4404 sits adjacent to 4403 (the
	// meshtasticd TCP transport) — "4403 talks to the radio, 4404
	// talks to clients of meshx".
	viper.SetDefault("server.bind", "127.0.0.1:4404")
	viper.SetDefault("server.radio", "")
}

// initLogger swaps the package-level logger to a tint handler with
// color when stderr is a TTY, plain text otherwise. --json swaps in
// the slog JSON handler — for log aggregators that prefer structured
// input. Level follows --debug.
func initLogger() {
	level := slog.LevelInfo
	if viper.GetBool("debug") {
		level = slog.LevelDebug
	}

	var handler slog.Handler
	if jsonOutput {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	} else {
		handler = tint.NewHandler(os.Stderr, &tint.Options{
			Level:      level,
			TimeFormat: time.Kitchen,
			NoColor:    !term.IsTerminal(int(os.Stderr.Fd())),
		})
	}

	logger = slog.New(handler)
	slog.SetDefault(logger)
}
