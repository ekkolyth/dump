package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ekkolyth/dump/internal/components"
	driveutil "github.com/ekkolyth/dump/internal/drives"
	"github.com/ekkolyth/dump/internal/transfer"
	"github.com/ekkolyth/dump/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type wizardStep int

const (
	stepSourceSelect wizardStep = iota
	stepDestSelect
	stepClientInput
	stepEventInput
	stepConfirm
	stepResumeSelect
	stepCleanSelect
	stepTransfer
)

// transferEventMsg wraps a transfer.TransferEvent for the Bubble Tea message loop.
type transferEventMsg transfer.TransferEvent

type model struct {
	step wizardStep

	// Drive data
	allDrives []driveutil.DiskInfo

	// Step 1: Source selection
	sourceList components.DriveListModel

	// Step 2: Destination drive selection
	destList     components.DriveListModel
	destIndexMap []int // maps dest list indices back to allDrives indices
	destPath     string

	// Step 3-4: Client and event name input
	clientName string
	eventName  string
	textInput  string // current text input buffer

	// Step 5: Confirmation
	selectedSources []driveutil.DiskInfo
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
	status string
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

func InitialModel() model {
	drives, err := driveutil.DiscoverDrives()
	if err != nil {
		return model{err: fmt.Sprintf("Failed to discover drives: %v", err)}
	}

	driveInfos := make([]components.DriveInfo, len(drives))
	for i, d := range drives {
		driveInfos[i] = components.DriveInfo{
			VolumeName:     d.VolumeName,
			MountPoint:     d.MountPoint,
			DeviceID:       d.DeviceIdentifier,
			TotalSize:      driveutil.FormatSize(d.TotalSize),
			FreeSpace:      driveutil.FormatSize(d.EffectiveFreeSpace()),
			FilesystemName: d.FilesystemName,
			IsExternal:     d.IsExternal(),
		}
	}

	srcList := components.NewDriveList(driveInfos, true)
	srcList.ExtraItems = []string{"Resume Session", "Clean Drives"}

	return model{
		step:       stepSourceSelect,
		allDrives:  drives,
		sourceList: srcList,
	}
}

func ResumeModel(sessionID string) model {
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
	engine, err := transfer.NewEngineResume(ctx, sessionID, cards, dest.MountPoint, "", transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
	if err != nil {
		cancel()
		return model{err: fmt.Sprintf("Resume failed: %v", err)}
	}

	completed := engine.CompletedStats()
	dashCards := make([]components.CardProgress, len(engine.Cards))
	for i, c := range engine.Cards {
		dashCards[i] = components.CardProgress{
			CardName:   fmt.Sprintf("card-%d", c.CardIndex+1),
			VolumeName: c.VolumeName,
			TotalFiles: c.TotalFiles,
			TotalBytes: c.TotalBytes,
		}
		if s, ok := completed[i]; ok {
			dashCards[i].CompletedFiles = s.Files
			dashCards[i].BytesDone = s.Bytes
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
		engine, err := transfer.NewEngineResume(ctx, sessionID, sources, destPath, "", transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
		if err != nil {
			cancel()
			m.err = fmt.Sprintf("Resume failed: %v", err)
			m.step = stepSourceSelect
			return m, nil
		}

		m.engine = engine
		m.cancelEngine = cancel
		m.sessionID = sessionID

		completed := engine.CompletedStats()
		dashCards := make([]components.CardProgress, len(engine.Cards))
		for i, c := range engine.Cards {
			dashCards[i] = components.CardProgress{
				CardName:   fmt.Sprintf("card-%d", c.CardIndex+1),
				VolumeName: c.VolumeName,
				TotalFiles: c.TotalFiles,
				TotalBytes: c.TotalBytes,
			}
			if s, ok := completed[i]; ok {
				dashCards[i].CompletedFiles = s.Files
				dashCards[i].BytesDone = s.Bytes
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
			// q no longer quits from the transfer done screen; use the menu instead
			return m, nil
		}
	}

	switch m.step {
	case stepSourceSelect:
		return m.updateSourceSelect(msg)
	case stepDestSelect:
		return m.updateDestSelect(msg)
	case stepClientInput:
		return m.updateClientInput(msg)
	case stepEventInput:
		return m.updateEventInput(msg)
	case stepConfirm:
		return m.updateConfirm(msg)
	case stepResumeSelect:
		return m.updateResumeSelect(msg)
	case stepCleanSelect:
		return m.updateCleanSelect(msg)
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
	case stepClientInput:
		m.step = stepDestSelect
	case stepEventInput:
		m.step = stepClientInput
	case stepConfirm:
		m.step = stepEventInput
	case stepResumeSelect, stepCleanSelect:
		m.step = stepSourceSelect
	}
	return m, nil
}

func (m model) updateSourceSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case components.ExtraItemSelectedMsg:
		if msg.Label == "Resume Session" || msg.Label == "Clean Drives" {
			var targetStep wizardStep
			if msg.Label == "Resume Session" {
				targetStep = stepResumeSelect
			} else {
				targetStep = stepCleanSelect
			}
			m.step = targetStep
			var drives []components.DriveInfo
			for _, d := range m.allDrives {
				drives = append(drives, components.DriveInfo{
					VolumeName:     d.VolumeName,
					MountPoint:     d.MountPoint,
					DeviceID:       d.DeviceIdentifier,
					TotalSize:      driveutil.FormatSize(d.TotalSize),
					FreeSpace:      driveutil.FormatSize(d.EffectiveFreeSpace()),
					FilesystemName: d.FilesystemName,
					IsExternal:     d.IsExternal(),
				})
			}
			m.destList = components.NewDriveList(drives, true)
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
				TotalSize:      driveutil.FormatSize(d.TotalSize),
				FreeSpace:      driveutil.FormatSize(d.EffectiveFreeSpace()),
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

func (m model) updateCleanSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case components.DriveSelectedMsg:
		if len(msg.Selected) == 0 {
			return m, nil
		}

		var cleaned []string
		for _, idx := range msg.Selected {
			drive := m.allDrives[idx]
			transfer.RemoveDumpMetadata(drive.MountPoint)
			transfer.RemoveProgressFile(drive.MountPoint)
			cleaned = append(cleaned, drive.VolumeName)
		}

		m.status = fmt.Sprintf("Cleaned %d drive(s): %s", len(cleaned), strings.Join(cleaned, ", "))
		m.step = stepSourceSelect
		return m, nil
	default:
		m.destList, cmd = m.destList.Update(msg)
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
			m.textInput = ""
			m.step = stepClientInput
		}
		return m, nil
	default:
		m.destList, cmd = m.destList.Update(msg)
	}

	return m, cmd
}


func (m model) updateClientInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.textInput != "" {
				m.clientName = m.textInput
				m.textInput = ""
				m.step = stepEventInput
			}
		case "backspace":
			if len(m.textInput) > 0 {
				m.textInput = m.textInput[:len(m.textInput)-1]
			}
		default:
			if len(msg.String()) == 1 {
				m.textInput += msg.String()
			}
		}
	}
	return m, nil
}

func (m model) updateEventInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.textInput != "" {
				m.eventName = m.textInput
				m.textInput = ""
				// Now build card summaries and go to confirm
				m.cardSummaries = nil
				now := time.Now()
				datePrefix := fmt.Sprintf("%s - %s - %s", now.Format("06.01.02"), m.clientName, m.eventName)
				for i, src := range m.selectedSources {
					files, _ := transfer.DiscoverMediaFiles(src.MountPoint)
					var totalBytes int64
					for _, f := range files {
						totalBytes += f.Size
					}
					m.cardSummaries = append(m.cardSummaries, cardSummary{
						Name:       fmt.Sprintf("%s - CARD %d", datePrefix, i+1),
						FileCount:  len(files),
						TotalBytes: totalBytes,
					})
				}
				m.step = stepConfirm
			}
		case "backspace":
			if len(m.textInput) > 0 {
				m.textInput = m.textInput[:len(m.textInput)-1]
			}
		default:
			if len(msg.String()) == 1 {
				m.textInput += msg.String()
			}
		}
	}
	return m, nil
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
	now := time.Now()
	eventFolder := fmt.Sprintf("%s - %s - %s", now.Format("06.01.02"), m.clientName, m.eventName)

	cards := make([]transfer.CardSource, len(m.selectedSources))
	for i, src := range m.selectedSources {
		cards[i] = transfer.CardSource{
			MountPoint: src.MountPoint,
			VolumeName: src.VolumeName,
			CardIndex:  i,
			FolderName: fmt.Sprintf("CARD %d", i+1),
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	engine, err := transfer.NewEngine(ctx, cards, m.destPath, eventFolder, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
	if err != nil {
		cancel()
		m.err = err.Error()
		return m, nil
	}

	completedNew := engine.CompletedStats()
	dashCards := make([]components.CardProgress, len(engine.Cards))
	for i, c := range engine.Cards {
		dashCards[i] = components.CardProgress{
			CardName:   fmt.Sprintf("card-%d", i+1),
			VolumeName: c.VolumeName,
			TotalFiles: c.TotalFiles,
			TotalBytes: c.TotalBytes,
		}
		if s, ok := completedNew[i]; ok {
			dashCards[i].CompletedFiles = s.Files
			dashCards[i].BytesDone = s.Bytes
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
		if m.dashboard.AllDone {
			switch msg.String() {
			case "up", "k":
				m.dashboard.PostCursorUp()
			case "down", "j":
				m.dashboard.PostCursorDown()
			case "enter":
				return m.handlePostDoneChoice()
			}
		} else {
			switch msg.String() {
			case "up", "k":
				m.dashboard.ScrollUp()
			case "down", "j":
				m.dashboard.ScrollDown()
			}
		}
	}

	return m, nil
}

// deleteSourceCards removes all files from the input drives that were part of this dump.
func (m *model) deleteSourceCards() {
	if m.engine == nil {
		return
	}
	for _, card := range m.engine.Cards {
		entries, err := os.ReadDir(card.MountPoint)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			// Skip hidden files/dirs (e.g. .Spotlight, .Trashes, .fseventsd)
			if strings.HasPrefix(name, ".") {
				continue
			}
			path := filepath.Join(card.MountPoint, name)
			os.RemoveAll(path)
		}
	}
}

func (m model) handlePostDoneChoice() (tea.Model, tea.Cmd) {
	switch m.dashboard.PostCursor {
	case components.PostDoneDeleteCards:
		m.deleteSourceCards()
		m.status = fmt.Sprintf("Deleted files from %d card(s)", len(m.engine.Cards))
		// Re-discover drives and go back to main menu
		return m.resetToMainMenu()
	case components.PostDoneDeleteAndExit:
		m.deleteSourceCards()
		return m, tea.Quit
	case components.PostDoneBackToMenu:
		return m.resetToMainMenu()
	}
	return m, nil
}

func (m model) resetToMainMenu() (tea.Model, tea.Cmd) {
	drives, err := driveutil.DiscoverDrives()
	if err != nil {
		m.err = fmt.Sprintf("Failed to discover drives: %v", err)
		return m, nil
	}

	driveInfos := make([]components.DriveInfo, len(drives))
	for i, d := range drives {
		driveInfos[i] = components.DriveInfo{
			VolumeName:     d.VolumeName,
			MountPoint:     d.MountPoint,
			DeviceID:       d.DeviceIdentifier,
			TotalSize:      driveutil.FormatSize(d.TotalSize),
			FreeSpace:      driveutil.FormatSize(d.EffectiveFreeSpace()),
			FilesystemName: d.FilesystemName,
			IsExternal:     d.IsExternal(),
		}
	}

	srcList := components.NewDriveList(driveInfos, true)
	srcList.ExtraItems = []string{"Resume Session", "Clean Drives"}

	m.allDrives = drives
	m.sourceList = srcList
	m.engine = nil
	m.cancelEngine = nil
	m.sessionID = ""
	m.step = stepSourceSelect
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
		if card.ActiveFiles == nil {
			card.ActiveFiles = make(map[string]*components.ActiveFile)
		}
		if _, exists := card.ActiveFiles[evt.File.RelPath]; !exists {
			card.ActiveFiles[evt.File.RelPath] = &components.ActiveFile{
				RelPath: evt.File.RelPath,
			}
		}

	case transfer.EventFileProgress:
		if af, ok := card.ActiveFiles[evt.File.RelPath]; ok {
			af.Speed = evt.Progress.Speed
			af.Pct = evt.Progress.Percentage
			af.BytesTransferred = evt.Progress.BytesTransferred
		}

	case transfer.EventFileSizeMismatch:
		m.dashboard.AddLogEntry(components.LogWarning,
			fmt.Sprintf("%s/%s  SIZE MISMATCH — %s", card.CardName, evt.File.RelPath, evt.Err.Error()))

	case transfer.EventFileComplete:
		card.CompletedFiles++
		card.BytesDone += evt.File.Size
		delete(card.ActiveFiles, evt.File.RelPath)
		if card.CompletedFiles+card.FailedFiles >= card.TotalFiles {
			card.Done = true
		}
		m.dashboard.AddLogEntry(components.LogComplete,
			fmt.Sprintf("%s/%s  (%s)", m.dashboard.Cards[idx].CardName, evt.File.RelPath, formatSizeShort(evt.File.Size)))

	case transfer.EventFileRetry:
		m.dashboard.AddLogEntry(components.LogRetry,
			fmt.Sprintf("%s/%s  retry %d/%d", card.CardName, evt.File.RelPath, evt.Retry, evt.MaxRetry))

	case transfer.EventFileFailed:
		delete(card.ActiveFiles, evt.File.RelPath)
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
		b.WriteString(titleInline.Render("Dump v"+version.Version) + "  " + helpInline.Render("space: toggle | enter: select | esc: quit"))
		b.WriteString("\n")
		b.WriteString("\n")
		b.WriteString("Welcome, Mel and/or Cass!")
		b.WriteString("\n\n")
		if m.status != "" {
			b.WriteString(confirmKey.Render("  ✓ "+m.status) + "\n\n")
		}
		b.WriteString(titleStyle.Render("New Dump — Select Source Cards"))
		b.WriteString("\n")
		b.WriteString(m.sourceList.View())

	case stepCleanSelect:
		b.WriteString(titleInline.Render("Dump v"+version.Version) + "  " + helpInline.Render("space: toggle | enter: clean | esc: back"))
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render("Clean Drives — Remove Transfer Metadata"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Select drives to remove dump.json and progress files from"))
		b.WriteString("\n\n")
		b.WriteString(m.destList.View())

	case stepResumeSelect:
		b.WriteString(titleInline.Render("Dump v"+version.Version) + "  " + helpInline.Render("space: toggle | enter: confirm | esc: back"))
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render("Resume Session — Select Drives"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Select all drives that belong to the session"))
		b.WriteString("\n\n")
		b.WriteString(m.destList.View())

	case stepDestSelect:
		b.WriteString(titleInline.Render("Dump v"+version.Version) + "  " + helpInline.Render("space: select | enter: confirm | esc: back"))
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render("Step 2 — Select Destination Drive"))
		b.WriteString("\n")
		b.WriteString(m.destList.View())

	case stepClientInput:
		b.WriteString(titleInline.Render("Dump v"+version.Version) + "  " + helpInline.Render("type client name | enter: confirm | esc: back"))
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render("Step 3 — Client Name"))
		b.WriteString("\n\n")
		b.WriteString(fmt.Sprintf("  > %s█", m.textInput))

	case stepEventInput:
		b.WriteString(titleInline.Render("Dump v"+version.Version) + "  " + helpInline.Render("type event name | enter: confirm | esc: back"))
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render("Step 4 — Event Name"))
		b.WriteString("\n")
		b.WriteString(helpInline.Render(fmt.Sprintf("  Client: %s", m.clientName)))
		b.WriteString("\n\n")
		b.WriteString(fmt.Sprintf("  > %s█", m.textInput))

	case stepConfirm:
		b.WriteString(titleInline.Render("Dump v"+version.Version) + "  " + helpInline.Render("enter: start import | esc: back"))
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render("Step 5 — Confirm Import"))
		b.WriteString("\n")

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(1, 2)
		if m.width > 0 {
			boxStyle = boxStyle.Width(m.width - 6)
		}

		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#AD8CFF")).Bold(true)
		valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8A0BF"))
		cardStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8A0BF"))
		totalStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6AD5"))

		var content strings.Builder

		now := time.Now()
		eventFolder := fmt.Sprintf("%s - %s - %s", now.Format("06.01.02"), m.clientName, m.eventName)
		content.WriteString(labelStyle.Render("Event") + "      " + valueStyle.Render(eventFolder) + "\n")
		content.WriteString(labelStyle.Render("Destination") + "  " + valueStyle.Render(m.destPath) + "\n")

		content.WriteString("\n")
		content.WriteString(labelStyle.Render("Cards") + "\n")
		totalFiles := 0
		var totalBytes int64
		for _, s := range m.cardSummaries {
			totalFiles += s.FileCount
			totalBytes += s.TotalBytes
			content.WriteString(cardStyle.Render(fmt.Sprintf("  %s", s.Name)))
			content.WriteString(helpInline.Render(fmt.Sprintf("  %d files  %s", s.FileCount, driveutil.FormatSize(s.TotalBytes))))
			content.WriteString("\n")
		}

		content.WriteString("\n")
		content.WriteString(totalStyle.Render(fmt.Sprintf("Total: %d files  %s", totalFiles, driveutil.FormatSize(totalBytes))))

		b.WriteString(boxStyle.Render(content.String()))
		b.WriteString("\n\n")
		b.WriteString(confirmKey.Render("  Press Enter to start import"))

	case stepTransfer:
		b.WriteString(m.dashboard.View())
	}

	return b.String()
}
