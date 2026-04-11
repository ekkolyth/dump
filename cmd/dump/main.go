package main

import (
	"fmt"
	"os"
	"syscall"

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
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if tui.WantsUpgrade(finalModel) {
		fmt.Println()
		if err := runUpgrade(); err != nil {
			fmt.Fprintf(os.Stderr, "Upgrade failed: %v\n", err)
			os.Exit(1)
		}
		// Re-exec the new binary
		binary, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\nRestarting dump...")
		syscall.Exec(binary, []string{"dump"}, os.Environ())
	}
}

func runUpgrade() error {
	return upgrade.Run()
}
