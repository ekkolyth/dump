package transfer

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const MaxConcurrentDefault = 2
const MaxRetriesDefault = 3

type CardSource struct {
	MountPoint string
	VolumeName string
	CardIndex  int
	FolderName string // card folder name, e.g. "CARD 1"
	Files      []MediaFile
	TotalFiles int
	TotalBytes int64
}

type TransferJob struct {
	File      MediaFile
	CardIndex int
	Retries   int
	Dest      string
}

type JobQueue struct {
	mu   sync.Mutex
	jobs []*TransferJob
}

func NewJobQueue() *JobQueue {
	return &JobQueue{}
}

func (q *JobQueue) Push(j *TransferJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, j)
}

func (q *JobQueue) Pop() *TransferJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.jobs) == 0 {
		return nil
	}
	j := q.jobs[0]
	q.jobs = q.jobs[1:]
	return j
}

func (q *JobQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

func VolumeMissing(mountPoint string) bool {
	_, err := os.Stat(mountPoint)
	return os.IsNotExist(err)
}

type TransferEvent struct {
	Type      EventType
	CardIndex int
	File      MediaFile
	Progress  Progress
	Err       error
	Retry     int
	MaxRetry  int
}

type EventType int

const (
	EventFileStart EventType = iota
	EventFileProgress
	EventFileComplete
	EventFileRetry
	EventFileFailed
	EventFileSizeMismatch
	EventCardPaused
	EventCardWaiting
	EventCardResumed
	EventAllComplete
)

type Engine struct {
	Cards         []CardSource
	DestBase      string
	EventFolder   string // parent folder for all cards, e.g. "26.04.04 - CLIENT - EVENT"
	MaxConcurrent int
	MaxRetries    int
	SessionID     string
	Events        chan TransferEvent
	queue         *JobQueue
	ctx           context.Context
	progress      *ProgressTracker
	mu            sync.RWMutex // protects Cards[].MountPoint and DestBase
}

func NewEngine(ctx context.Context, cards []CardSource, destBase, eventFolder string, maxConcurrent, maxRetries int) (*Engine, error) {
	e := &Engine{
		DestBase:      destBase,
		EventFolder:   eventFolder,
		MaxConcurrent: maxConcurrent,
		MaxRetries:    maxRetries,
		Events:        make(chan TransferEvent, 100),
		queue:         NewJobQueue(),
		ctx:           ctx,
	}

	for i := range cards {
		files, err := DiscoverMediaFiles(cards[i].MountPoint)
		if err != nil {
			return nil, fmt.Errorf("discover files on %s: %w", cards[i].VolumeName, err)
		}

		cards[i].Files = files
		cards[i].TotalFiles = len(files)
		var totalBytes int64
		for _, f := range files {
			totalBytes += f.Size
		}
		cards[i].TotalBytes = totalBytes

		cardDir := cards[i].FolderName
		if cardDir == "" {
			cardDir = fmt.Sprintf("CARD %d", cards[i].CardIndex+1)
		}
		for _, f := range files {
			dest := filepath.Join(destBase, eventFolder, cardDir, f.RelPath)
			e.queue.Push(&TransferJob{
				File:      f,
				CardIndex: cards[i].CardIndex,
				Dest:      dest,
			})
		}
	}

	e.Cards = cards

	sessionID := GenerateSessionID()
	e.SessionID = sessionID
	startedAt := time.Now().UTC().Format(time.RFC3339)

	for i, card := range cards {
		if err := WriteDumpMetadata(card.MountPoint, DumpMetadata{
			SessionID: sessionID,
			Role:      "source",
			CardIndex: i,
			CardName:  card.VolumeName,
			StartedAt: startedAt,
		}); err != nil {
			return nil, fmt.Errorf("write source metadata for %s: %w", card.VolumeName, err)
		}
	}

	sourceCardIDs := make([]int, len(cards))
	for i := range cards {
		sourceCardIDs[i] = i
	}
	if err := WriteDumpMetadata(destBase, DumpMetadata{
		SessionID:     sessionID,
		Role:          "destination",
		SourceCardIDs: sourceCardIDs,
		StartedAt:     startedAt,
	}); err != nil {
		return nil, fmt.Errorf("write destination metadata: %w", err)
	}

	e.progress = NewProgressTracker(destBase, sessionID)

	return e, nil
}

// NewEngineResume creates an engine that resumes an existing session.
// It uses the existing session ID and skips writing new dump.json files.
// Already-completed files (from dump-progress.json) are skipped during Run().
func NewEngineResume(ctx context.Context, sessionID string, cards []CardSource, destBase, eventFolder string, maxConcurrent, maxRetries int) (*Engine, error) {
	e := &Engine{
		DestBase:      destBase,
		EventFolder:   eventFolder,
		MaxConcurrent: maxConcurrent,
		MaxRetries:    maxRetries,
		SessionID:     sessionID,
		Events:        make(chan TransferEvent, 100),
		queue:         NewJobQueue(),
		ctx:           ctx,
	}

	// Load existing progress
	e.progress = NewProgressTracker(destBase, sessionID)

	for i := range cards {
		files, err := DiscoverMediaFiles(cards[i].MountPoint)
		if err != nil {
			return nil, fmt.Errorf("discover files on %s: %w", cards[i].VolumeName, err)
		}

		cards[i].Files = files
		cards[i].TotalFiles = len(files)
		var totalBytes int64
		for _, f := range files {
			totalBytes += f.Size
		}
		cards[i].TotalBytes = totalBytes

		cardDir := cards[i].FolderName
		if cardDir == "" {
			cardDir = fmt.Sprintf("CARD %d", cards[i].CardIndex+1)
		}
		for _, f := range files {
			dest := filepath.Join(destBase, eventFolder, cardDir, f.RelPath)
			e.queue.Push(&TransferJob{
				File:      f,
				CardIndex: cards[i].CardIndex,
				Dest:      dest,
			})
		}
	}

	e.Cards = cards
	return e, nil
}

// CompletedStats returns per-card completed file count and bytes for pre-populating the dashboard.
func (e *Engine) CompletedStats() map[int]struct{ Files int; Bytes int64 } {
	stats := make(map[int]struct{ Files int; Bytes int64 })
	for i, card := range e.Cards {
		var files int
		var bytes int64
		for _, f := range card.Files {
			if e.progress.IsComplete(card.CardIndex, f.RelPath) {
				files++
				bytes += f.Size
			}
		}
		if files > 0 {
			stats[i] = struct{ Files int; Bytes int64 }{files, bytes}
		}
	}
	return stats
}

// waitForVolume polls for a volume matching the session's dump.json.
// Returns the mount point when found, or error if context is cancelled.
func (e *Engine) waitForVolume(scanRoot, role string, cardIndex int) (string, error) {
	for {
		mount, found := FindVolumeBySession(scanRoot, e.SessionID, role, cardIndex)
		if found {
			return mount, nil
		}
		select {
		case <-e.ctx.Done():
			return "", e.ctx.Err()
		case <-time.After(2 * time.Second):
			// poll again
		}
	}
}

// ensureVolume checks if a volume is present via dump.json scan. If missing,
// emits EventCardWaiting and polls until it reappears or context is cancelled.
func (e *Engine) ensureVolume(scanRoot, role string, cardIndex int, displayName string) (string, error) {
	mount, found := FindVolumeBySession(scanRoot, e.SessionID, role, cardIndex)
	if found {
		return mount, nil
	}

	e.Events <- TransferEvent{
		Type:      EventCardWaiting,
		CardIndex: cardIndex,
		Err:       fmt.Errorf("%s disconnected", displayName),
	}

	mount, err := e.waitForVolume(scanRoot, role, cardIndex)
	if err != nil {
		return "", err
	}

	e.Events <- TransferEvent{
		Type:      EventCardResumed,
		CardIndex: cardIndex,
	}

	return mount, nil
}

func (e *Engine) Run() {
	defer close(e.Events)

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.MaxConcurrent)

	for {
		select {
		case <-e.ctx.Done():
			wg.Wait()
			return
		default:
		}

		job := e.queue.Pop()
		if job == nil {
			break
		}

		// Skip already-completed files (for resume)
		if e.progress.IsComplete(job.CardIndex, job.File.RelPath) {
			continue
		}

		sem <- struct{}{}
		wg.Add(1)

		go func(j *TransferJob) {
			defer wg.Done()
			defer func() { <-sem }()
			e.processJob(j)
		}(job)
	}

	wg.Wait()
	e.Events <- TransferEvent{Type: EventAllComplete}
}

func (e *Engine) processJob(j *TransferJob) {
	card := &e.Cards[j.CardIndex]
	scanRoot := VolumesRoot

	// Outer loop: wait for volumes, then attempt transfer
	for {
		// Ensure source volume is present (waits indefinitely)
		if err := e.ensureAndUpdateSource(scanRoot, j, card); err != nil {
			return // context cancelled
		}

		// Ensure destination volume is present (waits indefinitely)
		if err := e.ensureAndUpdateDest(scanRoot, j); err != nil {
			return // context cancelled
		}

		destDir := filepath.Dir(j.Dest)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			e.Events <- TransferEvent{
				Type:      EventFileFailed,
				CardIndex: j.CardIndex,
				File:      j.File,
				Err:       fmt.Errorf("create dest dir: %w", err),
			}
			return
		}

		e.Events <- TransferEvent{
			Type:      EventFileStart,
			CardIndex: j.CardIndex,
			File:      j.File,
		}

		// Inner loop: retry rsync up to MaxRetries for non-disconnection errors
		disconnected := false
		for attempt := 0; attempt <= e.MaxRetries; attempt++ {
			if attempt > 0 {
				// Check if this failure was a disconnection
				e.mu.RLock()
				srcMissing := VolumeMissing(card.MountPoint)
				destMissing := VolumeMissing(e.DestBase)
				e.mu.RUnlock()

				if srcMissing || destMissing {
					disconnected = true
					break // back to outer loop for reconnection wait
				}

				backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
				select {
				case <-e.ctx.Done():
					return
				case <-time.After(backoff):
				}

				e.Events <- TransferEvent{
					Type:      EventFileRetry,
					CardIndex: j.CardIndex,
					File:      j.File,
					Retry:     attempt,
					MaxRetry:  e.MaxRetries,
				}
			}

			err := RsyncFile(j.File.AbsPath, j.Dest, func(p Progress) {
				e.Events <- TransferEvent{
					Type:      EventFileProgress,
					CardIndex: j.CardIndex,
					File:      j.File,
					Progress:  p,
				}
			})

			if err == nil {
				// Verify destination file size matches source
				if destInfo, statErr := os.Stat(j.Dest); statErr == nil && destInfo.Size() != j.File.Size {
					e.Events <- TransferEvent{
						Type:      EventFileSizeMismatch,
						CardIndex: j.CardIndex,
						File:      j.File,
						Err:       fmt.Errorf("size mismatch: source %d bytes, dest %d bytes", j.File.Size, destInfo.Size()),
					}
				}
				e.Events <- TransferEvent{
					Type:      EventFileComplete,
					CardIndex: j.CardIndex,
					File:      j.File,
				}
				e.progress.MarkComplete(j.CardIndex, j.File.RelPath)
				return
			}

			if attempt == e.MaxRetries {
				e.Events <- TransferEvent{
					Type:      EventFileFailed,
					CardIndex: j.CardIndex,
					File:      j.File,
					Err:       err,
				}
				return
			}
		}

		if !disconnected {
			return
		}
		// disconnected = true: loop back to outer wait
	}
}

// ensureAndUpdateSource waits for the source volume and updates mount paths under lock.
func (e *Engine) ensureAndUpdateSource(scanRoot string, j *TransferJob, card *CardSource) error {
	e.mu.RLock()
	currentMount := card.MountPoint
	e.mu.RUnlock()

	newMount, err := e.ensureVolume(scanRoot, "source", j.CardIndex, card.VolumeName)
	if err != nil {
		return err
	}
	if newMount != currentMount {
		e.mu.Lock()
		card.MountPoint = newMount
		e.mu.Unlock()
		// Update job's absolute path using prefix replacement
		if strings.HasPrefix(j.File.AbsPath, currentMount) {
			j.File.AbsPath = newMount + j.File.AbsPath[len(currentMount):]
		}
	}
	return nil
}

// ensureAndUpdateDest waits for the destination volume and updates paths under lock.
func (e *Engine) ensureAndUpdateDest(scanRoot string, j *TransferJob) error {
	e.mu.RLock()
	currentDest := e.DestBase
	e.mu.RUnlock()

	newDest, err := e.ensureVolume(scanRoot, "destination", -1, "destination")
	if err != nil {
		return err
	}
	if newDest != currentDest {
		e.mu.Lock()
		e.DestBase = newDest
		e.mu.Unlock()
		if strings.HasPrefix(j.Dest, currentDest) {
			j.Dest = newDest + j.Dest[len(currentDest):]
		}
	}
	return nil
}
