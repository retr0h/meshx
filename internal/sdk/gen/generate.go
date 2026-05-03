// Package gen contains generated code for the meshx HTTP API client.
//
// The api.yaml spec is sourced from a running daemon (curl
// localhost:8080/openapi.yaml > api.yaml) and committed alongside
// the generated client. Regenerate with `go generate ./internal/sdk/gen/`
// after the spec changes.
package gen

//go:generate go tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config cfg.yaml api.yaml
