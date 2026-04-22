# VHS tape: tmux status bar preview.
#
# Render from the repo root with:
#   vhs asset/tmux-bar.tape
#
# Produces asset/tmux-bar.gif — a tight GIF of the progress bar
# draining over ~10 seconds, then pulsing hot-pink for ~5 seconds of
# the expiry alert. Uses `meshx status --ansi` so the colors render
# directly in a plain terminal (no tmux required).

Output asset/tmux-bar.gif

Set Shell "zsh"
Set FontSize 16
Set Width 780
Set Height 48
Set Padding 6
Set TypingSpeed 10ms

# VHS starts zsh with --no-rcs, so kube_ps1/aws_prof/etc. don't expand.
# Source the user's rc first so plugin-heavy prompts (kube_ps1, aws_prof,
# etc.) finish initializing before we try to launch anything. 3s of
# slack covers typical Oh-My-Zsh plugin setup.
Hide
Type "source ~/.zshrc"
Enter
Sleep 3s

# Schedule meshx to start in 2 seconds — that gives us time to type the
# display loop without burning visible timer seconds. The subshell
# decouples meshx from our foreground prompt so we can keep typing.
Type "(sleep 2 && meshx --bar --timer 10s) >/dev/null 2>&1 &"
Enter
Sleep 200ms

Type "clear"
Enter
Sleep 100ms

# Start the redraw loop while state.json doesn't yet exist — the loop
# prints nothing until meshx actually launches ~2s later.
Type `while :; do clear; meshx status --ansi; sleep 1; done`
Enter

# Wait until the delayed meshx has started and the loop has rendered
# the first frame. Show right around that moment so frame 0 ≈ 10s left.
Sleep 2300ms
Show

# ~10s of drain + ~5s of pulse.
Sleep 15s

# Clean up the background process so the user's state file doesn't linger.
Ctrl+C
Sleep 200ms
Type "meshx stop"
Enter
