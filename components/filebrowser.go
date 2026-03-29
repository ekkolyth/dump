package components

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FolderSelectedMsg is sent when the user confirms a directory selection.
type FolderSelectedMsg struct {
	Path string
}

// FileBrowserModel lets the user browse and select a directory.
type FileBrowserModel struct {
	currentPath string
	entries     []os.DirEntry
	cursor      int
	creating    bool // true when "create new folder" input is active
	newName     string
	err         string
	width       int
	height      int
}

func NewFileBrowser(rootPath string) FileBrowserModel {
	m := FileBrowserModel{currentPath: rootPath}
	m.loadEntries()
	return m
}

func (m *FileBrowserModel) loadEntries() {
	entries, err := os.ReadDir(m.currentPath)
	if err != nil {
		m.err = err.Error()
		m.entries = nil
		return
	}

	m.err = ""
	m.entries = nil
	for _, e := range entries {
		// Only directories, skip hidden
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			m.entries = append(m.entries, e)
		}
	}
	sort.Slice(m.entries, func(i, j int) bool {
		return m.entries[i].Name() < m.entries[j].Name()
	})
	m.cursor = 0
}

func (m FileBrowserModel) Init() tea.Cmd {
	return nil
}

func (m FileBrowserModel) Update(msg tea.Msg) (FileBrowserModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.creating {
			return m.updateCreating(msg)
		}

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			// +1 for ".." entry, +1 for "create new folder"
			maxCursor := len(m.entries) + 1 // 0="..", 1..N=entries, N+1="create"
			if m.cursor < maxCursor {
				m.cursor++
			}
		case " ":
			if m.cursor == 0 {
				// ".." — go up
				parent := filepath.Dir(m.currentPath)
				if parent != m.currentPath {
					m.currentPath = parent
					m.loadEntries()
				}
			} else if m.cursor <= len(m.entries) {
				// Drill into directory
				entry := m.entries[m.cursor-1]
				m.currentPath = filepath.Join(m.currentPath, entry.Name())
				m.loadEntries()
			} else {
				// "Create new folder" option
				m.creating = true
				m.newName = ""
			}
		case "enter":
			// Select the highlighted directory
			selectedPath := m.currentPath
			if m.cursor > 0 && m.cursor <= len(m.entries) {
				selectedPath = filepath.Join(m.currentPath, m.entries[m.cursor-1].Name())
			}
			return m, func() tea.Msg {
				return FolderSelectedMsg{Path: selectedPath}
			}
		case "esc":
			// Go up one level
			parent := filepath.Dir(m.currentPath)
			if parent != m.currentPath {
				m.currentPath = parent
				m.loadEntries()
			}
		}
	}
	return m, nil
}

func (m FileBrowserModel) updateCreating(msg tea.KeyMsg) (FileBrowserModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.newName != "" {
			newPath := filepath.Join(m.currentPath, m.newName)
			if err := os.MkdirAll(newPath, 0755); err != nil {
				m.err = fmt.Sprintf("Failed to create folder: %v", err)
			} else {
				m.currentPath = newPath
				m.loadEntries()
			}
		}
		m.creating = false
		m.newName = ""
	case "esc":
		m.creating = false
		m.newName = ""
	case "backspace":
		if len(m.newName) > 0 {
			m.newName = m.newName[:len(m.newName)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.newName += msg.String()
		}
	}
	return m, nil
}

var (
	breadcrumb   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).MarginBottom(1)
	dirCursor    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	createOption = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Italic(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	inputStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	browserHelp  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
)

func (m FileBrowserModel) View() string {
	var b strings.Builder

	// Breadcrumb
	b.WriteString(breadcrumb.Render("📂 " + m.currentPath))
	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(errorStyle.Render("Error: " + m.err))
		b.WriteString("\n")
	}

	// ".." entry
	cursor := "  "
	if m.cursor == 0 {
		cursor = "> "
	}
	line := fmt.Sprintf("%s  ..", cursor)
	if m.cursor == 0 {
		line = dirCursor.Render(line)
	}
	b.WriteString(line)
	b.WriteString("\n")

	// Directory entries
	for i, e := range m.entries {
		cursor = "  "
		if i+1 == m.cursor {
			cursor = "> "
		}
		line = fmt.Sprintf("%s  %s/", cursor, e.Name())
		if i+1 == m.cursor {
			line = dirCursor.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	// "Create new folder" option
	cursor = "  "
	createIdx := len(m.entries) + 1
	if m.cursor == createIdx {
		cursor = "> "
	}
	createLine := fmt.Sprintf("%s  + Create new folder", cursor)
	if m.cursor == createIdx {
		createLine = dirCursor.Render(createOption.Render(createLine))
	} else {
		createLine = createOption.Render(createLine)
	}
	b.WriteString(createLine)
	b.WriteString("\n")

	// Input mode
	if m.creating {
		b.WriteString("\n")
		b.WriteString(inputStyle.Render("  Folder name: " + m.newName + "█"))
		b.WriteString("\n")
	}

	// Help
	b.WriteString("\n")
	b.WriteString(browserHelp.Render("space: open folder • enter: select this folder • esc: go up"))

	return b.String()
}

// CurrentPath returns the path the browser is currently showing.
func (m FileBrowserModel) CurrentPath() string {
	return m.currentPath
}
