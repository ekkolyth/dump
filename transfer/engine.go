package transfer

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const MaxConcurrentDefault = 2
const MaxRetriesDefault = 5

type CardSource struct {
	MountPoint string
	VolumeName string
	CardIndex  int
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
	EventCardPaused
	EventAllComplete
)

type Engine struct {
	Cards         []CardSource
	DestBase      string
	MaxConcurrent int
	MaxRetries    int
	SessionID     string
	Events        chan TransferEvent
	queue         *JobQueue
	ctx           context.Context
	progress      *ProgressTracker
}

func NewEngine(ctx context.Context, cards []CardSource, destBase string, maxConcurrent, maxRetries int) (*Engine, error) {
	e := &Engine{
		DestBase:      destBase,
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

		cardDir := fmt.Sprintf("card-%d-%s", cards[i].CardIndex+1, cards[i].VolumeName)
		for _, f := range files {
			dest := filepath.Join(destBase, cardDir, f.RelPath)
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

func (e *Engine) Run() {
	defer close(e.Events)

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.MaxConcurrent)

	for {
		job := e.queue.Pop()
		if job == nil {
			break
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

	if VolumeMissing(card.MountPoint) {
		e.Events <- TransferEvent{
			Type:      EventCardPaused,
			CardIndex: j.CardIndex,
			File:      j.File,
			Err:       fmt.Errorf("volume %s disconnected", card.VolumeName),
		}
		e.Events <- TransferEvent{
			Type:      EventFileFailed,
			CardIndex: j.CardIndex,
			File:      j.File,
			Err:       fmt.Errorf("volume %s disconnected", card.VolumeName),
		}
		return
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

	for attempt := 0; attempt <= e.MaxRetries; attempt++ {
		if attempt > 0 {
			if VolumeMissing(card.MountPoint) {
				e.Events <- TransferEvent{
					Type:      EventCardPaused,
					CardIndex: j.CardIndex,
					File:      j.File,
					Err:       fmt.Errorf("volume %s disconnected", card.VolumeName),
				}
				e.Events <- TransferEvent{
					Type:      EventFileFailed,
					CardIndex: j.CardIndex,
					File:      j.File,
					Err:       fmt.Errorf("volume disconnected during retry"),
				}
				return
			}

			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			time.Sleep(backoff)

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
			e.Events <- TransferEvent{
				Type:      EventFileComplete,
				CardIndex: j.CardIndex,
				File:      j.File,
			}
			return
		}

		if attempt == e.MaxRetries {
			e.Events <- TransferEvent{
				Type:      EventFileFailed,
				CardIndex: j.CardIndex,
				File:      j.File,
				Err:       err,
			}
		}
	}
}
