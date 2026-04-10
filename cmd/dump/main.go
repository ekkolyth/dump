package main

import (
	"fmt"
	"os"

	"github.com/ekkolyth/dump/internal/tui"
	"github.com/ekkolyth/dump/internal/upgrade"
	"github.com/ekkolyth/dump/internal/version"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Printf("dump v%s\n", version.Version)
			return
		case "upgrade":
			if err := runUpgrade(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "resume":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "Usage: dump resume <session-id>")
				os.Exit(1)
			}
			runTUI(tui.ResumeModel(os.Args[2]))
			return
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: dump [version|upgrade|resume <id>]\n", os.Args[1])
			os.Exit(1)
		}
	}

	fmt.Printf("dump v%s\n", version.Version)
	fmt.Print("Scanning local drives...")
	m := tui.InitialModel()
	fmt.Println(" done")
	runTUI(m)
}

func runTUI(m tea.Model) {
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runUpgrade() error {
	return upgrade.Run()
}
