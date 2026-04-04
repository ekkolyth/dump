package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	resumeID := flag.String("resume", "", "Resume a previous session by ID")
	flag.Parse()

	var m model
	if *resumeID != "" {
		m = resumeModel(*resumeID)
	} else {
		m = initialModel()
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
