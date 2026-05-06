package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/titusmd/vaultmount/internal/tui"
)

func main() {
	p := tea.NewProgram(
		tui.NewAppModel(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "vaultmount: %v\n", err)
		os.Exit(1)
	}
}
