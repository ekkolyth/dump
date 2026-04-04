package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ekkolyth/dump/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	resumeID := flag.String("resume", "", "Resume a previous session by ID")
	flag.Parse()

	var m tea.Model
	if *resumeID != "" {
		m = tui.ResumeModel(*resumeID)
	} else {
		m = tui.InitialModel()
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
