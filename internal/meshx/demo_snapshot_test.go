package meshx

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSnapshotView prints the full View() string to test output so we
// can eyeball the actual render without spawning a TTY.
func TestSnapshotView(_ *testing.T) {
	m := initialModel()
	m.mode = modeInput
	m.input.Focus()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = updated.(model)
	fmt.Println("──── snapshot (input) ────")
	fmt.Println(m.View())
	fmt.Println("──── end ────")
}
