package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DriveInfo holds the data needed to display a drive in the list.
type DriveInfo struct {
	VolumeName     string
	MountPoint     string
	DeviceID       string
	TotalSize      string // Pre-formatted, e.g. "119.0 GB"
	FreeSpace      string
	FilesystemName string
	IsExternal     bool
}

// DriveSelectedMsg is sent when the user confirms their selection.
type DriveSelectedMsg struct {
	Selected []int // Indices of selected drives
}

// DriveListModel is a multi-select or single-select list of drives.
type DriveListModel struct {
	Drives      []DriveInfo
	cursor      int
	selected    map[int]bool
	MultiSelect bool
	width       int
	height      int
}

func NewDriveList(drives []DriveInfo, multiSelect bool) DriveListModel {
	return DriveListModel{
		Drives:      drives,
		selected:    make(map[int]bool),
		MultiSelect: multiSelect,
	}
}

func (m DriveListModel) Init() tea.Cmd {
	return nil
}

func (m DriveListModel) Update(msg tea.Msg) (DriveListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.Drives)-1 {
				m.cursor++
			}
		case " ":
			if m.MultiSelect {
				m.selected[m.cursor] = !m.selected[m.cursor]
				if !m.selected[m.cursor] {
					delete(m.selected, m.cursor)
				}
			} else {
				// Single select: clear previous, select current
				m.selected = map[int]bool{m.cursor: true}
			}
		case "enter":
			if len(m.selected) > 0 {
				indices := make([]int, 0, len(m.selected))
				for i := range m.selected {
					indices = append(indices, i)
				}
				return m, func() tea.Msg { return DriveSelectedMsg{Selected: indices} }
			}
		}
	}
	return m, nil
}

var (
	driveCursor   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6AD5"))
	driveSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("#AD8CFF"))
	driveExtLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#F25D94")).Bold(true)
	driveIntLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C6C6C"))
)

func (m DriveListModel) View() string {
	var b strings.Builder

	for i, d := range m.Drives {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		check := "[ ] "
		if m.selected[i] {
			check = driveSelected.Render("[✓]") + " "
		}

		typeLabel := driveIntLabel.Render("INT")
		if d.IsExternal {
			typeLabel = driveExtLabel.Render("EXT")
		}

		line := fmt.Sprintf("%s%s%s  %-20s  %-8s  %10s free / %10s  %s",
			cursor,
			check,
			typeLabel,
			d.VolumeName,
			d.FilesystemName,
			d.FreeSpace,
			d.TotalSize,
			d.MountPoint,
		)

		if i == m.cursor {
			line = driveCursor.Render(line)
		} else if m.selected[i] {
			line = driveSelected.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// SelectedIndices returns the currently selected drive indices.
func (m DriveListModel) SelectedIndices() []int {
	indices := make([]int, 0, len(m.selected))
	for i := range m.selected {
		indices = append(indices, i)
	}
	return indices
}

// HasSelection returns true if at least one drive is selected.
func (m DriveListModel) HasSelection() bool {
	return len(m.selected) > 0
}
