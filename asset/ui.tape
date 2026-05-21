# VHS tape: meshX TUI screenshot.
#
# Render from the repo root with:
#   MESHX_BLE_UUID=<your-uuid> vhs asset/ui.tape
#
# Requires a radio already connected (or favorite BLE set).
# Produces asset/ui.gif and asset/ui.png (last frame screenshot).

Output asset/ui.gif

Set Shell "zsh"
Set FontSize 13
Set FontFamily "JetBrainsMono Nerd Font"
Set Width 1280
Set Height 720
Set Padding 12
Set Theme "Builtin Dark"
Set TypingSpeed 10ms

Hide
Type "source ~/.zshrc"
Enter
Sleep 2s
# Force truecolor so lipgloss renders the full maxheadroom palette.
Type "export COLORTERM=truecolor TERM=xterm-256color"
Enter
Sleep 200ms
Type "clear"
Enter
Sleep 200ms

# Connect to the BLE radio. UUID comes from the environment.
Type "go run . ble connect ${MESHX_BLE_UUID}"
Enter
# Wait for handshake + NodeDB sync + messages to populate.
Sleep 12s
Show

# Capture a few seconds of the live TUI with messages flowing.
Sleep 3s

Screenshot asset/ui.png
