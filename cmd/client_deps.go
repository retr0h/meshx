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
	"net/http"
	"os"
	"strings"

	"github.com/spf13/viper"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// clientConfig captures the resolved persistent flags for a client
// subcommand — pulled once at RunE entry so subcommands don't each
// re-read viper. Empty AuthToken is allowed (loopback daemons may
// run unauthenticated).
type clientConfig struct {
	ServerURL string
	AuthToken string // empty when no --auth-token-file / MESHX_CLIENT_AUTH_TOKEN_FILE
}

// resolveClientConfig collapses the persistent --server / --auth-
// token-file flags + their env-var counterparts into one struct.
// Token-file resolution reads the file once at command start; clients
// don't re-read mid-run because daemons rotate tokens by restart, not
// in-place.
func resolveClientConfig() (clientConfig, error) {
	cfg := clientConfig{
		ServerURL: viper.GetString("client.server"),
	}
	if cfg.ServerURL == "" {
		return cfg, fmt.Errorf(
			"client: --server URL required (or set MESHX_CLIENT_SERVER)",
		)
	}
	tokenFile := viper.GetString("client.auth_token_file")
	if tokenFile == "" {
		return cfg, nil
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return cfg, fmt.Errorf("client: read auth-token-file %s: %w", tokenFile, err)
	}
	cfg.AuthToken = strings.TrimSpace(string(raw))
	if cfg.AuthToken == "" {
		return cfg, fmt.Errorf("client: auth-token-file %s is empty", tokenFile)
	}
	return cfg, nil
}

// newSDKClient builds a typed HTTP client against cfg.ServerURL with
// the bearer token (if any) pre-wired via WithRequestEditorFn. Every
// outbound request from this client carries `Authorization: Bearer
// <token>` when AuthToken is set; an unauthed daemon (loopback bind)
// works equally well with AuthToken="".
func newSDKClient(cfg clientConfig) (*gen.ClientWithResponses, error) {
	opts := []gen.ClientOption{}
	if cfg.AuthToken != "" {
		token := cfg.AuthToken
		opts = append(opts, gen.WithRequestEditorFn(
			func(_ context.Context, req *http.Request) error {
				req.Header.Set("Authorization", "Bearer "+token)
				return nil
			},
		))
	}
	c, err := gen.NewClientWithResponses(cfg.ServerURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("client: build SDK: %w", err)
	}
	return c, nil
}
