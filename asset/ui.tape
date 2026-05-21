# VHS tape: meshX TUI demo.
#
# Render from the repo root with:
#   MESHX_BLE_UUID=<your-uuid> vhs asset/ui.tape
#
# Requires a paired BLE radio powered on.
# Produces asset/ui.gif.

Output asset/ui.gif

Set Shell "zsh"
Set FontSize 14
Set LineHeight 1.0
Set Width 1280
Set Height 720
Set Padding 16
Set TypingSpeed 10ms

Hide
Type "source ~/.zshrc"
Enter
Sleep 3s
Type "clear"
Enter
Sleep 200ms

Type "go run . ble connect ${MESHX_BLE_UUID}"
Enter
Sleep 12s
Show

Sleep 3s
