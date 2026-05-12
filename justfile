# Optional modules: mod? allows `just fetch` to work before .just/remote/ exists.
mod? go '.just/remote/go.mod.just'
mod? docs '.just/remote/docs.mod.just'
mod? just '.just/remote/just.mod.just'

# --- Fetch ---

# Fetch shared justfiles from osapi-justfiles
fetch:
    mkdir -p .just/remote
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/go.mod.just -o .just/remote/go.mod.just
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/go.just -o .just/remote/go.just
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/docs.mod.just -o .just/remote/docs.mod.just
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/docs.just -o .just/remote/docs.just
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/just.mod.just -o .just/remote/just.mod.just
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/just.just -o .just/remote/just.just

# --- Top-level orchestration ---

# Install all dependencies
deps:
    just go::deps
    just go::mod
    just docs::deps

# Run all tests
test:
    just just::fmt-check
    just go::test

# Generate code — sequenced so each stage reads the previous stage's
# output: (1) dumpspec + oapi-codegen write api.yaml + client.gen.go,

# (2) emoji widths (independent), (3) mcpgen reads api.yaml → tools_gen.go.
generate:
    go generate ./internal/sdk/gen/...
    go generate ./internal/tui/emoji/...
    go generate ./internal/mcp/...

# Format, lint before committing
ready:
    just generate
    just just::fmt
    just docs::fmt
    just go::fmt
    just go::vet
