// transfer/session.go
package transfer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// DumpMetadata is written to dump.json on each drive in a session.
type DumpMetadata struct {
	SessionID     string `json:"session_id"`
	Role          string `json:"role"` // "source" or "destination"
	CardIndex     int    `json:"card_index,omitempty"`
	CardName      string `json:"card_name,omitempty"`
	SourceCardIDs []int  `json:"source_card_ids,omitempty"`
	StartedAt     string `json:"started_at"`
}

// ProgressMetadata is written to dump-progress.json on the destination drive.
type ProgressMetadata struct {
	SessionID string           `json:"session_id"`
	Completed map[int][]string `json:"completed"` // card_index -> []relPath
}

// GenerateSessionID returns a random 8-character hex string.
func GenerateSessionID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

const metadataFile = "dump.json"
const progressFile = "dump-progress.json"

// WriteDumpMetadata writes dump.json to the root of a drive.
func WriteDumpMetadata(mountPoint string, meta DumpMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(mountPoint, metadataFile), data, 0644)
}

// ReadDumpMetadata reads dump.json from the root of a drive.
func ReadDumpMetadata(mountPoint string) (DumpMetadata, error) {
	data, err := os.ReadFile(filepath.Join(mountPoint, metadataFile))
	if err != nil {
		return DumpMetadata{}, err
	}
	var meta DumpMetadata
	err = json.Unmarshal(data, &meta)
	return meta, err
}

// RemoveDumpMetadata removes dump.json from a drive.
func RemoveDumpMetadata(mountPoint string) error {
	return os.Remove(filepath.Join(mountPoint, metadataFile))
}

// ProgressTracker tracks completed files and persists to dump-progress.json.
type ProgressTracker struct {
	mu        sync.Mutex
	destPath  string
	sessionID string
	completed map[int][]string
}

// NewProgressTracker creates a tracker. If dump-progress.json exists on the
// destination, it loads the existing state.
func NewProgressTracker(destPath, sessionID string) *ProgressTracker {
	pt := &ProgressTracker{
		destPath:  destPath,
		sessionID: sessionID,
		completed: make(map[int][]string),
	}
	// Try to load existing progress
	data, err := os.ReadFile(filepath.Join(destPath, progressFile))
	if err == nil {
		var meta ProgressMetadata
		if json.Unmarshal(data, &meta) == nil && meta.SessionID == sessionID {
			pt.completed = meta.Completed
		}
	}
	return pt
}

// MarkComplete records a file as completed and persists to disk.
func (pt *ProgressTracker) MarkComplete(cardIndex int, relPath string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.completed[cardIndex] = append(pt.completed[cardIndex], relPath)
	pt.persist()
}

// IsComplete returns true if a file has already been transferred.
func (pt *ProgressTracker) IsComplete(cardIndex int, relPath string) bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	for _, p := range pt.completed[cardIndex] {
		if p == relPath {
			return true
		}
	}
	return false
}

// CompletedSet returns the full map of completed files.
func (pt *ProgressTracker) CompletedSet() map[int][]string {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	out := make(map[int][]string, len(pt.completed))
	for k, v := range pt.completed {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

func (pt *ProgressTracker) persist() {
	meta := ProgressMetadata{
		SessionID: pt.sessionID,
		Completed: pt.completed,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(pt.destPath, progressFile), data, 0644)
}

// RemoveProgressFile removes dump-progress.json from the destination.
func RemoveProgressFile(destPath string) error {
	return os.Remove(filepath.Join(destPath, progressFile))
}
