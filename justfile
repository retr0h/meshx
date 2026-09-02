set allow-duplicate-variables

# Optional modules: import? allows `just fetch` to work before .just/remote/ exists.

import? '.just/remote/go.just'
import? '.just/remote/md.just'
import? '.just/remote/just.just'

# No documentation site, so md formats every markdown file in the repository.

md_site_dir := ""

# The go module defaults this to 100. meshx is not there yet; declaring the
# current floor keeps the gate meaningful and ratchets up as coverage grows.
# CI reports 44.9%; 44 leaves headroom for variance between toolchains.

go_coverage_target := "44"

# --- Fetch ---

# Fetch shared justfiles from osapi-justfiles
fetch:
    mkdir -p .just/remote
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/go/go.just -o .just/remote/go.just
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/md/md.just -o .just/remote/md.just
    curl -sSfL https://raw.githubusercontent.com/osapi-io/osapi-justfiles/refs/heads/main/just/just.just -o .just/remote/just.just

# --- Top-level orchestration ---

# Install all dependencies
deps:
    just go-deps
    just go-mod

# Run all tests
test:
    just just-fmt-check
    just md-fmt-check
    just go-test

# Format, lint before committing
ready:
    just just-fmt
    just md-fmt
    just go-fmt
    just go-vet
