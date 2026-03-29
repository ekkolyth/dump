package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mikekenway/sdcard-dump/components"
	"github.com/mikekenway/sdcard-dump/transfer"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type wizardStep int

const (
	stepSourceSelect wizardStep = iota
	stepDestSelect
	stepDestBrowse
	stepConfirm
	stepTransfer
)

// transferEventMsg wraps a transfer.TransferEvent for the Bubble Tea message loop.
type transferEventMsg transfer.TransferEvent

type model struct {
	step wizardStep

	// Drive data
	allDrives []DiskInfo

	// Step 1: Source selection
	sourceList components.DriveListModel

	// Step 2: Destination drive selection + file browser
	destList       components.DriveListModel
	destIndexMap   []int // maps dest list indices back to allDrives indices
	fileBrowser    components.FileBrowserModel
	destPath       string

	// Step 3: Confirmation
	selectedSources []DiskInfo
	cardSummaries   []cardSummary

	// Step 4: Transfer
	dashboard components.DashboardModel
	engine    *transfer.Engine

	// Layout
	width  int
	height int
	err    string
}

type cardSummary struct {
	Name       string
	FileCount  int
	TotalBytes int64
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).MarginBottom(1)
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	confirmKey = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
)

func initialModel() model {
	drives, err := DiscoverDrives()
	if err != nil {
		return model{err: fmt.Sprintf("Failed to discover drives: %v", err)}
	}

	driveInfos := make([]components.DriveInfo, len(drives))
	for i, d := range drives {
		driveInfos[i] = components.DriveInfo{
			VolumeName:     d.VolumeName,
			MountPoint:     d.MountPoint,
			DeviceID:       d.DeviceIdentifier,
			TotalSize:      FormatSize(d.TotalSize),
			FreeSpace:      FormatSize(d.EffectiveFreeSpace()),
			FilesystemName: d.FilesystemName,
			IsExternal:     d.IsExternal(),
		}
	}

	return model{
		step:       stepSourceSelect,
		allDrives:  drives,
		sourceList: components.NewDriveList(driveInfos, true),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dashboard.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.handleBack()
		case "q":
			if m.step == stepTransfer && m.dashboard.AllDone {
				return m, tea.Quit
			}
		}
	}

	switch m.step {
	case stepSourceSelect:
		return m.updateSourceSelect(msg)
	case stepDestSelect:
		return m.updateDestSelect(msg)
	case stepDestBrowse:
		return m.updateDestBrowse(msg)
	case stepConfirm:
		return m.updateConfirm(msg)
	case stepTransfer:
		return m.updateTransfer(msg)
	}

	return m, nil
}

func (m model) handleBack() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepSourceSelect:
		return m, tea.Quit
	case stepDestSelect:
		m.step = stepSourceSelect
	case stepDestBrowse:
		m.step = stepDestSelect
	case stepConfirm:
		m.step = stepDestBrowse
	}
	return m, nil
}

func (m model) updateSourceSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case components.DriveSelectedMsg:
		m.selectedSources = nil
		indices := msg.Selected
		sort.Ints(indices)
		selectedSet := make(map[int]bool)
		for _, i := range indices {
			m.selectedSources = append(m.selectedSources, m.allDrives[i])
			selectedSet[i] = true
		}

		// Build dest list excluding selected sources
		var destDrives []components.DriveInfo
		m.destIndexMap = nil
		for i, d := range m.allDrives {
			if selectedSet[i] {
				continue
			}
			destDrives = append(destDrives, components.DriveInfo{
				VolumeName:     d.VolumeName,
				MountPoint:     d.MountPoint,
				DeviceID:       d.DeviceIdentifier,
				TotalSize:      FormatSize(d.TotalSize),
				FreeSpace:      FormatSize(d.EffectiveFreeSpace()),
				FilesystemName: d.FilesystemName,
				IsExternal:     d.IsExternal(),
			})
			m.destIndexMap = append(m.destIndexMap, i)
		}
		m.destList = components.NewDriveList(destDrives, false)

		m.step = stepDestSelect
		return m, nil
	default:
		m.sourceList, cmd = m.sourceList.Update(msg)
	}

	return m, cmd
}

func (m model) updateDestSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case components.DriveSelectedMsg:
		if len(msg.Selected) > 0 {
			destIdx := msg.Selected[0]
			driveIdx := m.destIndexMap[destIdx]
			mountPoint := m.allDrives[driveIdx].MountPoint
			m.fileBrowser = components.NewFileBrowser(mountPoint)
			m.step = stepDestBrowse
		}
		return m, nil
	default:
		m.destList, cmd = m.destList.Update(msg)
	}

	return m, cmd
}

func (m model) updateDestBrowse(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case components.FolderSelectedMsg:
		m.destPath = msg.Path
		m.cardSummaries = nil
		for i, src := range m.selectedSources {
			files, _ := transfer.DiscoverMediaFiles(src.MountPoint)
			var totalBytes int64
			for _, f := range files {
				totalBytes += f.Size
			}
			m.cardSummaries = append(m.cardSummaries, cardSummary{
				Name:       fmt.Sprintf("card-%d-%s", i+1, src.VolumeName),
				FileCount:  len(files),
				TotalBytes: totalBytes,
			})
		}
		m.step = stepConfirm
		return m, nil
	default:
		m.fileBrowser, cmd = m.fileBrowser.Update(msg)
	}

	return m, cmd
}

func (m model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "enter" {
			return m.startTransfer()
		}
	}
	return m, nil
}

func (m model) startTransfer() (tea.Model, tea.Cmd) {
	cards := make([]transfer.CardSource, len(m.selectedSources))
	for i, src := range m.selectedSources {
		cards[i] = transfer.CardSource{
			MountPoint: src.MountPoint,
			VolumeName: src.VolumeName,
			CardIndex:  i,
		}
	}

	engine, err := transfer.NewEngine(cards, m.destPath, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}

	dashCards := make([]components.CardProgress, len(engine.Cards))
	for i, c := range engine.Cards {
		dashCards[i] = components.CardProgress{
			CardName:   fmt.Sprintf("card-%d", i+1),
			VolumeName: c.VolumeName,
			TotalFiles: c.TotalFiles,
			TotalBytes: c.TotalBytes,
		}
	}

	m.engine = engine
	m.dashboard = components.NewDashboard(dashCards)
	m.step = stepTransfer

	return m, func() tea.Msg {
		go m.engine.Run()
		evt, ok := <-m.engine.Events
		if !ok {
			return transferEventMsg{Type: transfer.EventAllComplete}
		}
		return transferEventMsg(evt)
	}
}

func (m model) updateTransfer(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case transferEventMsg:
		evt := transfer.TransferEvent(msg)
		m.applyTransferEvent(evt)

		if evt.Type == transfer.EventAllComplete {
			m.dashboard.AllDone = true
			return m, nil
		}

		return m, func() tea.Msg {
			evt, ok := <-m.engine.Events
			if !ok {
				return transferEventMsg{Type: transfer.EventAllComplete}
			}
			return transferEventMsg(evt)
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.dashboard.ScrollUp()
		case "down", "j":
			m.dashboard.ScrollDown()
		}
	}

	return m, nil
}

func (m *model) applyTransferEvent(evt transfer.TransferEvent) {
	idx := evt.CardIndex
	if idx < 0 || idx >= len(m.dashboard.Cards) {
		return
	}

	card := &m.dashboard.Cards[idx]

	switch evt.Type {
	case transfer.EventFileStart:
		card.CurrentFile = evt.File.RelPath

	case transfer.EventFileProgress:
		card.CurrentFile = evt.File.RelPath
		card.CurrentSpeed = evt.Progress.Speed
		card.CurrentPct = evt.Progress.Percentage

	case transfer.EventFileComplete:
		card.CompletedFiles++
		card.BytesDone += evt.File.Size
		card.CurrentFile = ""
		card.CurrentSpeed = ""
		if card.CompletedFiles+card.FailedFiles >= card.TotalFiles {
			card.Done = true
		}
		m.dashboard.AddLogEntry(components.LogComplete,
			fmt.Sprintf("%s/%s  (%s)", m.dashboard.Cards[idx].CardName, evt.File.RelPath, formatSizeShort(evt.File.Size)))

	case transfer.EventFileRetry:
		m.dashboard.AddLogEntry(components.LogRetry,
			fmt.Sprintf("%s/%s  retry %d/%d", card.CardName, evt.File.RelPath, evt.Retry, evt.MaxRetry))

	case transfer.EventFileFailed:
		card.FailedFiles++
		if card.CompletedFiles+card.FailedFiles >= card.TotalFiles {
			card.Done = true
		}
		errMsg := ""
		if evt.Err != nil {
			errMsg = " — " + evt.Err.Error()
		}
		m.dashboard.AddLogEntry(components.LogFailed,
			fmt.Sprintf("%s/%s  FAILED%s", card.CardName, evt.File.RelPath, errMsg))

	case transfer.EventCardPaused:
		card.Paused = true
		m.dashboard.AddLogEntry(components.LogWarning,
			fmt.Sprintf("%s: volume disconnected", card.VolumeName))
	}
}

func formatSizeShort(b int64) string {
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

func (m model) View() string {
	if m.err != "" {
		return errStyle.Render("Error: "+m.err) + "\n"
	}

	var b strings.Builder

	switch m.step {
	case stepSourceSelect:
		b.WriteString(titleStyle.Render("Step 1/4 — Select Source Cards"))
		b.WriteString("\n")
		b.WriteString(m.sourceList.View())
		b.WriteString(helpStyle.Render("space: toggle • enter: confirm • esc: quit"))

	case stepDestSelect:
		b.WriteString(titleStyle.Render("Step 2/4 — Select Destination Drive"))
		b.WriteString("\n")
		b.WriteString(m.destList.View())
		b.WriteString(helpStyle.Render("space: select • enter: confirm • esc: back"))

	case stepDestBrowse:
		b.WriteString(titleStyle.Render("Step 2/4 — Choose Destination Folder"))
		b.WriteString("\n")
		b.WriteString(m.fileBrowser.View())

	case stepConfirm:
		b.WriteString(titleStyle.Render("Step 3/4 — Confirm Import"))
		b.WriteString("\n\n")

		b.WriteString("  Sources:\n")
		for _, s := range m.cardSummaries {
			b.WriteString(fmt.Sprintf("    • %s — %d files (%s)\n",
				s.Name, s.FileCount, FormatSize(s.TotalBytes)))
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  Destination: %s\n", m.destPath))
		b.WriteString("\n")

		totalFiles := 0
		var totalBytes int64
		for _, s := range m.cardSummaries {
			totalFiles += s.FileCount
			totalBytes += s.TotalBytes
		}
		b.WriteString(fmt.Sprintf("  Total: %d files, %s\n", totalFiles, FormatSize(totalBytes)))
		b.WriteString("\n")

		// Show media extensions for transparency
		b.WriteString("  Media extensions: ")
		exts := make([]string, 0, len(transfer.MediaExtensions))
		for ext := range transfer.MediaExtensions {
			exts = append(exts, ext)
		}
		sort.Strings(exts)
		b.WriteString(helpStyle.Render(strings.Join(exts, ", ")))
		b.WriteString("\n\n")

		b.WriteString(confirmKey.Render("  Press Enter to start import") + " • " + helpStyle.Render("esc: back"))

	case stepTransfer:
		b.WriteString(m.dashboard.View())
	}

	return b.String()
}
