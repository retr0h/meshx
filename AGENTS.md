# AGENTS.md

Test: `just test` | Before committing: `just ready`

Read [docs/development.md](docs/development.md) first. It covers setup, the just
recipes, the architecture and package layout, renderer and testing conventions,
and the color palette. [docs/contributing.md](docs/contributing.md) covers the
pull request workflow, commit messages and code style. All of it applies to
agents exactly as it applies to people. This file carries only what is specific
to agents.

## Running tools

Invoke tools through `mise`, not from your path:

```bash
mise exec -- just test
```

`mise` is active in a person's shell and supplies the versions `.mise.toml`
declares. An agent's shell has no activation, so a bare `just` resolves to
whatever is installed globally, usually an older version.

The symptom is a check that fails here and passes in continuous integration, on
a file nobody edited. When that happens, establish which version ran before
treating the failure as real.

## Screenshot

Recording `asset/ui.gif` needs a radio paired with the person's machine. Ask
them for its UUID rather than scanning for one, and leave the recording to them
if they have no radio to hand.

## Commit trailer

When committing via Claude Code, end the message with:

```
🤖 Generated with [Claude Code](https://claude.ai/code)

Co-Authored-By: Claude <noreply@anthropic.com>
```
