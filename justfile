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

# Format, lint before committing
ready:
    just just::fmt
    just docs::fmt
    just go::fmt
    just go::vet
