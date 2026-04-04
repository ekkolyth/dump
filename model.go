package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mikekenway/sdcard-dump/components"
	"github.com/mikekenway/sdcard-dump/transfer"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type wizardStep int

const (
	stepSourceSelect wizardStep = iota
	stepDestSelect
	stepConfirm
	stepResumeSelect
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

	// Step 2: Destination drive selection
	destList     components.DriveListModel
	destIndexMap []int // maps dest list indices back to allDrives indices
	destPath     string

	// Step 3: Confirmation
	selectedSources []DiskInfo
	cardSummaries   []cardSummary

	// Step 4: Transfer
	dashboard    components.DashboardModel
	engine       *transfer.Engine
	cancelEngine context.CancelFunc
	sessionID    string

	// Ctrl+C tracking
	lastCtrlC time.Time

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
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6AD5")).MarginBottom(1)
	titleInline   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6AD5"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).MarginTop(1)
	helpInline    = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Faint(true)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#F25D94"))
	confirmKey = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#AD8CFF"))
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

	srcList := components.NewDriveList(driveInfos, true)
	srcList.ExtraItems = []string{"Resume Session"}

	return model{
		step:       stepSourceSelect,
		allDrives:  drives,
		sourceList: srcList,
	}
}

func resumeModel(sessionID string) model {
	// Scan for all volumes belonging to this session
	matches := transfer.FindAllSessionVolumes(transfer.VolumesRoot, sessionID)
	if len(matches) == 0 {
		return model{err: fmt.Sprintf("No drives found for session %s. Plug in the drives and try again.", sessionID)}
	}

	// Separate source and destination
	var sources []transfer.VolumeMatch
	var dest *transfer.VolumeMatch
	for i, m := range matches {
		if m.Meta.Role == "destination" {
			dest = &matches[i]
		} else if m.Meta.Role == "source" {
			sources = append(sources, m)
		}
	}

	if dest == nil {
		return model{err: "Destination drive not found. Plug it in and try again."}
	}
	if len(sources) == 0 {
		return model{err: "No source drives found. Plug them in and try again."}
	}

	// Build card sources
	cards := make([]transfer.CardSource, len(sources))
	for i, src := range sources {
		cards[i] = transfer.CardSource{
			MountPoint: src.MountPoint,
			VolumeName: src.Meta.CardName,
			CardIndex:  src.Meta.CardIndex,
		}
	}

	// Create engine with existing session
	ctx, cancel := context.WithCancel(context.Background())
	engine, err := transfer.NewEngineResume(ctx, sessionID, cards, dest.MountPoint, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
	if err != nil {
		cancel()
		return model{err: fmt.Sprintf("Resume failed: %v", err)}
	}

	dashCards := make([]components.CardProgress, len(engine.Cards))
	for i, c := range engine.Cards {
		dashCards[i] = components.CardProgress{
			CardName:   fmt.Sprintf("card-%d", c.CardIndex+1),
			VolumeName: c.VolumeName,
			TotalFiles: c.TotalFiles,
			TotalBytes: c.TotalBytes,
		}
	}

	return model{
		step:         stepTransfer,
		engine:       engine,
		cancelEngine: cancel,
		sessionID:    sessionID,
		dashboard:    components.NewDashboard(dashCards),
	}
}


func (m model) updateResumeSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case components.DriveSelectedMsg:
		if len(msg.Selected) == 0 {
			return m, nil
		}

		// Read dump.json from each selected drive, group by session
		sessions := make(map[string][]transfer.VolumeMatch)
		for _, idx := range msg.Selected {
			drive := m.allDrives[idx]
			meta, err := transfer.ReadDumpMetadata(drive.MountPoint)
			if err != nil {
				continue
			}
			sessions[meta.SessionID] = append(sessions[meta.SessionID], transfer.VolumeMatch{
				MountPoint: drive.MountPoint,
				Meta:       meta,
			})
		}

		if len(sessions) == 0 {
			m.err = "No session data found on selected drives"
			m.step = stepSourceSelect
			return m, nil
		}

		// Use the first session found
		var sessionID string
		var matches []transfer.VolumeMatch
		for id, vol := range sessions {
			sessionID = id
			matches = vol
			break
		}

		// Separate sources and destination
		var sources []transfer.CardSource
		var destPath string
		for _, match := range matches {
			if match.Meta.Role == "destination" {
				destPath = match.MountPoint
			} else if match.Meta.Role == "source" {
				sources = append(sources, transfer.CardSource{
					MountPoint: match.MountPoint,
					VolumeName: match.Meta.CardName,
					CardIndex:  match.Meta.CardIndex,
				})
			}
		}

		if destPath == "" {
			m.err = "No destination drive found in selection"
			m.step = stepSourceSelect
			return m, nil
		}
		if len(sources) == 0 {
			m.err = "No source drives found in selection"
			m.step = stepSourceSelect
			return m, nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		engine, err := transfer.NewEngineResume(ctx, sessionID, sources, destPath, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
		if err != nil {
			cancel()
			m.err = fmt.Sprintf("Resume failed: %v", err)
			m.step = stepSourceSelect
			return m, nil
		}

		m.engine = engine
		m.cancelEngine = cancel
		m.sessionID = sessionID

		dashCards := make([]components.CardProgress, len(engine.Cards))
		for i, c := range engine.Cards {
			dashCards[i] = components.CardProgress{
				CardName:   fmt.Sprintf("card-%d", c.CardIndex+1),
				VolumeName: c.VolumeName,
				TotalFiles: c.TotalFiles,
				TotalBytes: c.TotalBytes,
			}
		}
		m.dashboard = components.NewDashboard(dashCards)
		m.dashboard.SetSize(m.width, m.height)
		m.step = stepTransfer

		return m, func() tea.Msg {
			go m.engine.Run()
			evt, ok := <-m.engine.Events
			if !ok {
				return transferEventMsg{Type: transfer.EventAllComplete}
			}
			return transferEventMsg(evt)
		}

	default:
		m.destList, cmd = m.destList.Update(msg)
	}

	return m, cmd
}

func (m model) Init() tea.Cmd {
	if m.step == stepTransfer && m.engine != nil {
		return func() tea.Msg {
			go m.engine.Run()
			evt, ok := <-m.engine.Events
			if !ok {
				return transferEventMsg{Type: transfer.EventAllComplete}
			}
			return transferEventMsg(evt)
		}
	}
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
			if m.step == stepTransfer && !m.dashboard.AllDone {
				now := time.Now()
				if !m.lastCtrlC.IsZero() && now.Sub(m.lastCtrlC) < 3*time.Second {
					// Double Ctrl+C — exit with resume code
					if m.cancelEngine != nil {
						m.cancelEngine()
					}
					return m, tea.Sequence(
						tea.Printf("\nTo resume this session: dump --resume %s\n", m.sessionID),
						tea.Quit,
					)
				}
				// First Ctrl+C — cancel engine, go back to source select
				m.lastCtrlC = now
				if m.cancelEngine != nil {
					m.cancelEngine()
				}
				m.step = stepSourceSelect
				m.engine = nil
				m.cancelEngine = nil
				return m, nil
			}
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
	case stepConfirm:
		return m.updateConfirm(msg)
	case stepResumeSelect:
		return m.updateResumeSelect(msg)
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
	case stepConfirm:
		m.step = stepDestSelect
	case stepResumeSelect:
		m.step = stepSourceSelect
	}
	return m, nil
}

func (m model) updateSourceSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case components.ExtraItemSelectedMsg:
		if msg.Label == "Resume Session" {
			m.step = stepResumeSelect
			var resumeDrives []components.DriveInfo
			for _, d := range m.allDrives {
				resumeDrives = append(resumeDrives, components.DriveInfo{
					VolumeName:     d.VolumeName,
					MountPoint:     d.MountPoint,
					DeviceID:       d.DeviceIdentifier,
					TotalSize:      FormatSize(d.TotalSize),
					FreeSpace:      FormatSize(d.EffectiveFreeSpace()),
					FilesystemName: d.FilesystemName,
					IsExternal:     d.IsExternal(),
				})
			}
			m.destList = components.NewDriveList(resumeDrives, true)
			return m, nil
		}
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
			m.destPath = mountPoint
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
		}
		return m, nil
	default:
		m.destList, cmd = m.destList.Update(msg)
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

	ctx, cancel := context.WithCancel(context.Background())
	engine, err := transfer.NewEngine(ctx, cards, m.destPath, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
	if err != nil {
		cancel()
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
	m.cancelEngine = cancel
	m.sessionID = engine.SessionID
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
			if m.engine != nil {
				for _, card := range m.engine.Cards {
					transfer.RemoveDumpMetadata(card.MountPoint)
				}
				transfer.RemoveDumpMetadata(m.engine.DestBase)
				transfer.RemoveProgressFile(m.engine.DestBase)
			}
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

	// Destination events use CardIndex -1 — handle them as log-only entries
	if idx < 0 || idx >= len(m.dashboard.Cards) {
		switch evt.Type {
		case transfer.EventCardWaiting:
			m.dashboard.AddLogEntry(components.LogWarning, "destination: waiting for reconnection...")
		case transfer.EventCardResumed:
			m.dashboard.AddLogEntry(components.LogReconnected, "destination: reconnected, resuming transfer")
		}
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

	case transfer.EventCardWaiting:
		card.Waiting = true
		card.WaitingFor = card.VolumeName
		m.dashboard.AddLogEntry(components.LogWarning,
			fmt.Sprintf("%s: waiting for reconnection...", card.VolumeName))

	case transfer.EventCardResumed:
		card.Waiting = false
		card.WaitingFor = ""
		m.dashboard.AddLogEntry(components.LogReconnected,
			fmt.Sprintf("%s: reconnected, resuming transfer", card.VolumeName))
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
		b.WriteString(titleInline.Render("Dump v0.0.1") + "  " + helpInline.Render("space: toggle | enter: select | esc: quit"))
		b.WriteString("\n")
		b.WriteString(helpInline.Render("Welcome, Mel and/or Cass!"))
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render("New Dump — Select Source Cards"))
		b.WriteString("\n")
		b.WriteString(m.sourceList.View())

	case stepResumeSelect:
		b.WriteString(titleInline.Render("Dump v0.0.1") + "  " + helpInline.Render("space: toggle | enter: confirm | esc: back"))
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("Resume Session — Select Drives"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Select all drives that belong to the session"))
		b.WriteString("\n\n")
		b.WriteString(m.destList.View())

	case stepDestSelect:
		b.WriteString(titleInline.Render("Dump v0.0.1") + "  " + helpInline.Render("space: select | enter: confirm | esc: back"))
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("Select Destination Drive"))
		b.WriteString("\n")
		b.WriteString(m.destList.View())

	case stepConfirm:
		b.WriteString(titleInline.Render("Dump v0.0.1") + "  " + helpInline.Render("enter: start import | esc: back"))
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("Confirm Import"))
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

		b.WriteString(confirmKey.Render("  Press Enter to start import"))

	case stepTransfer:
		b.WriteString(m.dashboard.View())
	}

	return b.String()
}
