package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// CardProgress tracks transfer progress for a single card.
type CardProgress struct {
	CardName       string
	VolumeName     string
	TotalFiles     int
	CompletedFiles int
	FailedFiles    int
	TotalBytes     int64
	BytesDone      int64
	CurrentFile    string
	CurrentSpeed   string
	CurrentPct     int
	Paused         bool
	Waiting        bool
	WaitingFor     string
	Done           bool
}

// LogEntry is a single line in the scrollable log.
type LogEntry struct {
	Time    time.Time
	Type    LogEntryType
	Message string
}

type LogEntryType int

const (
	LogComplete LogEntryType = iota
	LogRetry
	LogFailed
	LogWarning
	LogReconnected
)

// DashboardModel displays transfer progress and a scrollable log.
type DashboardModel struct {
	Cards     []CardProgress
	Log       []LogEntry
	logOffset int
	startTime time.Time
	width     int
	height    int
	AllDone   bool
}

func NewDashboard(cards []CardProgress) DashboardModel {
	return DashboardModel{
		Cards:     cards,
		startTime: time.Now(),
	}
}

// SetSize updates the terminal dimensions for layout.
func (m *DashboardModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// AddLogEntry appends a log entry and auto-scrolls to bottom.
func (m *DashboardModel) AddLogEntry(entryType LogEntryType, message string) {
	m.Log = append(m.Log, LogEntry{
		Time:    time.Now(),
		Type:    entryType,
		Message: message,
	})
	maxVisible := m.logVisibleLines()
	if len(m.Log) > maxVisible {
		m.logOffset = len(m.Log) - maxVisible
	}
}

func (m DashboardModel) logVisibleLines() int {
	lines := m.height - (len(m.Cards)*3 + 10)
	if lines < 5 {
		lines = 5
	}
	return lines
}

// ScrollUp scrolls the log up.
func (m *DashboardModel) ScrollUp() {
	if m.logOffset > 0 {
		m.logOffset--
	}
}

// ScrollDown scrolls the log down.
func (m *DashboardModel) ScrollDown() {
	maxOffset := len(m.Log) - m.logVisibleLines()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.logOffset < maxOffset {
		m.logOffset++
	}
}

var (
	dashBorder    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#874BFD")).Padding(1, 2)
	logBorder     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#874BFD")).Padding(0, 1)
	progressDone  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6AD5"))
	progressTodo  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3C3C3C"))
	cardName      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E8A0BF"))
	speedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C6C6C"))
	logSuccess    = lipgloss.NewStyle().Foreground(lipgloss.Color("#AD8CFF"))
	logRetryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8A0BF"))
	logFailStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F25D94"))
	logWarnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F25D94"))
	summaryStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6AD5")).MarginTop(1)
)

func (m DashboardModel) View() string {
	var b strings.Builder

	// Overall stats
	totalFiles, doneFiles, failedFiles := 0, 0, 0
	var totalBytes, doneBytes int64
	for _, c := range m.Cards {
		totalFiles += c.TotalFiles
		doneFiles += c.CompletedFiles
		failedFiles += c.FailedFiles
		totalBytes += c.TotalBytes
		doneBytes += c.BytesDone
	}

	title := fmt.Sprintf("Importing from %d card(s)", len(m.Cards))
	if m.AllDone {
		title = "Import Complete"
	}

	// Card progress section
	var cardsView strings.Builder
	for _, c := range m.Cards {
		label := cardName.Render(fmt.Sprintf("  %s (%s)", c.CardName, c.VolumeName))
		bar := renderProgressBar(c.CompletedFiles, c.TotalFiles, 30)
		stats := fmt.Sprintf("  %d/%d  %s", c.CompletedFiles, c.TotalFiles, formatBytes(c.BytesDone))

		cardsView.WriteString(label)
		cardsView.WriteString("      ")
		cardsView.WriteString(bar)
		cardsView.WriteString(stats)
		cardsView.WriteString("\n")

		if c.Done {
			if c.FailedFiles > 0 {
				cardsView.WriteString(fmt.Sprintf("    Done (%d failed)\n", c.FailedFiles))
			} else {
				cardsView.WriteString(progressDone.Render("    ✓ Complete") + "\n")
			}
		} else if c.Waiting {
			cardsView.WriteString(logWarnStyle.Render(fmt.Sprintf("    ⏳ Waiting for %s to reconnect...", c.WaitingFor)) + "\n")
		} else if c.Paused {
			cardsView.WriteString(logWarnStyle.Render("    ⚠ Volume disconnected") + "\n")
		} else if c.CurrentFile != "" {
			detail := fmt.Sprintf("    → %s", c.CurrentFile)
			if c.CurrentSpeed != "" {
				detail += speedStyle.Render(fmt.Sprintf("  (%s)", c.CurrentSpeed))
			}
			cardsView.WriteString(detail + "\n")
		}
		cardsView.WriteString("\n")
	}

	// Overall stats bar
	elapsed := time.Since(m.startTime).Round(time.Second)
	eta := ""
	if doneBytes > 0 && !m.AllDone {
		rate := float64(doneBytes) / time.Since(m.startTime).Seconds()
		remaining := float64(totalBytes-doneBytes) / rate
		eta = fmt.Sprintf("  ETA %s", time.Duration(remaining*float64(time.Second)).Round(time.Second))
	}
	overall := summaryStyle.Render(fmt.Sprintf(
		"  Overall: %d/%d files  ·  %s / %s  ·  %s elapsed%s",
		doneFiles, totalFiles,
		formatBytes(doneBytes), formatBytes(totalBytes),
		elapsed, eta,
	))

	cardsView.WriteString(overall)

	dashStyle := dashBorder
	logStyle := logBorder
	if m.width > 0 {
		// Width sets content width; subtract border (2) + horizontal padding to match terminal width
		// dashBorder has Padding(1,2) = 4 horizontal padding + 2 border = 6
		dashStyle = dashStyle.Width(m.width - 6)
		// logBorder has Padding(0,1) = 2 horizontal padding + 2 border = 4
		logStyle = logStyle.Width(m.width - 4)
	}

	b.WriteString(dashStyle.Render(title + "\n\n" + cardsView.String()))
	b.WriteString("\n")

	// Log section
	var logView strings.Builder
	visible := m.logVisibleLines()
	start := m.logOffset
	end := start + visible
	if end > len(m.Log) {
		end = len(m.Log)
	}

	for i := start; i < end; i++ {
		entry := m.Log[i]
		var prefix string
		switch entry.Type {
		case LogComplete:
			prefix = logSuccess.Render("  ✓")
		case LogRetry:
			prefix = logRetryStyle.Render("  ⟳")
		case LogFailed:
			prefix = logFailStyle.Render("  ✗")
		case LogWarning:
			prefix = logWarnStyle.Render("  ⚠")
		case LogReconnected:
			prefix = logSuccess.Render("  ↻")
		}
		logView.WriteString(fmt.Sprintf("%s %s\n", prefix, entry.Message))
	}

	if len(m.Log) == 0 {
		logView.WriteString("  Waiting for transfers...\n")
	}

	b.WriteString(logStyle.Render("Log\n" + logView.String()))

	if m.AllDone {
		b.WriteString("\n\n")
		summary := fmt.Sprintf("  %d succeeded", doneFiles-failedFiles)
		if failedFiles > 0 {
			summary += logFailStyle.Render(fmt.Sprintf("  ·  %d failed", failedFiles))
		}
		b.WriteString(summaryStyle.Render(summary))
		b.WriteString("\n  Press q to exit")
	}

	return b.String()
}

func renderProgressBar(done, total, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}
	filled := (done * width) / total
	if filled > width {
		filled = width
	}
	bar := progressDone.Render(strings.Repeat("█", filled))
	bar += progressTodo.Render(strings.Repeat("░", width-filled))
	return bar
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
