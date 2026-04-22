# VHS tape: full-screen coffee cup preview.
#
# Render from the repo root with:
#   vhs asset/cup.tape
#
# Produces asset/cup.gif — the full foreground UI draining a cup
# over 10 seconds, then pulsing hot-pink for 5 seconds of the expiry
# alert, before quitting vim-style with ZZ.

Output asset/cup.gif

Set Shell "zsh"
Set FontSize 14
Set Width 800
Set Height 560
Set Padding 16
Set TypingSpeed 10ms

# Launch meshx fully off-camera so frame 0 of the recording is the cup
# already drawn and starting to drain. Sourcing ~/.zshrc is needed
# because VHS starts zsh with --no-rcs — plugin-heavy prompts
# (kube_ps1, aws_prof) can take a couple of seconds to initialize.
Hide
Type "source ~/.zshrc"
Enter
Sleep 3s
Type "clear"
Enter
Sleep 200ms

# `sleep 1 && meshx` defers the actual timer start by 1s so we can line
# up `Show` with meshx's first paint — frame 0 is the cup near-full,
# not already mid-drain.
Type "sleep 1 && meshx --timer 10s"
Enter
Sleep 1100ms
Show

# 10s drain + 5s pulse = tight 15s demo, cup on screen from frame 0.
Sleep 15s

# Vim-style quit so the recording ends cleanly.
Type "ZZ"
Sleep 300ms
