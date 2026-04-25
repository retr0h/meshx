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
	"fmt"

	goversion "github.com/caarlos0/go-version"
	"github.com/spf13/cobra"
)

// Build-time identity. Populated by goreleaser via -ldflags -X at
// link time; left blank for `go run` / `go build` of a working
// tree (caarlos0/go-version backfills "(devel)" defaults from
// debug.ReadBuildInfo when the strings are empty).
var (
	version   = ""
	commit    = ""
	treeState = ""
	date      = ""
	builtBy   = ""
)

// buildVersion stitches the goreleaser ldflag values into a
// goversion.Info — printed by `meshx version` in the same JSON
// shape the rest of the retr0h tooling uses.
func buildVersion(version, commit, date, builtBy, treeState string) goversion.Info {
	return goversion.GetVersionInfo(
		goversion.WithAppDetails(
			"meshx",
			"a glitched-out terminal Meshtastic messenger.\n",
			"https://github.com/retr0h/meshx",
		),
		func(i *goversion.Info) {
			if commit != "" {
				i.GitCommit = commit
			}
			if treeState != "" {
				i.GitTreeState = treeState
			}
			if date != "" {
				i.BuildDate = date
			}
			if version != "" {
				i.GitVersion = version
			}
			if builtBy != "" {
				i.BuiltBy = builtBy
			}
		},
	)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display the version of meshx",
	Run: func(_ *cobra.Command, _ []string) {
		v := buildVersion(version, commit, date, builtBy, treeState)
		jsonOut, _ := v.JSONString()
		fmt.Println(jsonOut)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
