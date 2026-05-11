// Package gen contains generated code for the meshx HTTP API client.
//
// Two-stage regen, both invoked by `go generate ./internal/sdk/gen/`
// (or its wrapper `just generate`). No daemon required:
//
//  1. dumpspec — extracts the daemon's OpenAPI 3.0 spec directly
//     from Huma in-process (no listener, no port) and writes
//     api.yaml.
//  2. oapi-codegen — generates the typed Go HTTP client
//     (client.gen.go) from api.yaml + cfg.yaml.
//
// Both api.yaml and client.gen.go are committed; downstream consumers
// (the TUI's remote mode, future external clients) build straight
// against the vendored client without touching codegen.
package gen

//go:generate go tool github.com/retr0h/meshx/internal/sdk/gen/dumpspec -out api.yaml
//go:generate go tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config cfg.yaml api.yaml
