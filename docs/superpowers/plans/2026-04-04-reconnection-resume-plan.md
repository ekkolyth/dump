# Reconnection & Resume Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make transfers survive drive disconnection by waiting indefinitely for reconnection, and allow resuming interrupted sessions via CLI flag or interactive menu.

**Architecture:** The transfer engine gains a `context.Context` for cancellation and replaces its fixed retry loop with an indefinite volume-wait loop that scans for drives by `dump.json` identity files. A `dump-progress.json` on the destination tracks completed files. The TUI gets Ctrl+C handling (once = back to home, twice = exit with resume code) and a "Resume Session" home screen option.

**Tech Stack:** Go, Bubble Tea, rsync, JSON metadata files

---

### Task 1: Session and Metadata Types

**Files:**
- Create: `transfer/session.go`
- Test: `transfer/session_test.go`

- [ ] **Step 1: Write failing tests for session ID generation and metadata types**

```go
// transfer/session_test.go
package transfer

import (
	"encoding/json"
	"testing"
)

func TestGenerateSessionID(t *testing.T) {
	id1 := GenerateSessionID()
	id2 := GenerateSessionID()
	if len(id1) != 8 {
		t.Errorf("session ID length = %d, want 8", len(id1))
	}
	if id1 == id2 {
		t.Error("two generated session IDs should not be equal")
	}
}

func TestSourceMetadataMarshal(t *testing.T) {
	m := DumpMetadata{
		SessionID: "abc12345",
		Role:      "source",
		CardIndex: 0,
		CardName:  "card-1-CANON",
		StartedAt: "2026-04-04T14:30:00Z",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DumpMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SessionID != "abc12345" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "abc12345")
	}
	if got.Role != "source" {
		t.Errorf("Role = %q, want %q", got.Role, "source")
	}
	if got.CardIndex != 0 {
		t.Errorf("CardIndex = %d, want 0", got.CardIndex)
	}
}

func TestDestMetadataMarshal(t *testing.T) {
	m := DumpMetadata{
		SessionID:     "abc12345",
		Role:          "destination",
		SourceCardIDs: []int{0, 1},
		StartedAt:     "2026-04-04T14:30:00Z",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DumpMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Role != "destination" {
		t.Errorf("Role = %q, want %q", got.Role, "destination")
	}
	if len(got.SourceCardIDs) != 2 {
		t.Fatalf("SourceCardIDs len = %d, want 2", len(got.SourceCardIDs))
	}
}

func TestProgressMetadataMarshal(t *testing.T) {
	p := ProgressMetadata{
		SessionID: "abc12345",
		Completed: map[int][]string{
			0: {"DCIM/100CANON/IMG_0001.CR3"},
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProgressMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Completed[0]) != 1 {
		t.Fatalf("Completed[0] len = %d, want 1", len(got.Completed[0]))
	}
	if got.Completed[0][0] != "DCIM/100CANON/IMG_0001.CR3" {
		t.Errorf("Completed[0][0] = %q, want %q", got.Completed[0][0], "DCIM/100CANON/IMG_0001.CR3")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./transfer/ -run "TestGenerateSessionID|TestSourceMetadata|TestDestMetadata|TestProgressMetadata" -v`
Expected: FAIL — types and functions not defined

- [ ] **Step 3: Implement session types**

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./transfer/ -run "TestGenerateSessionID|TestSourceMetadata|TestDestMetadata|TestProgressMetadata" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add transfer/session.go transfer/session_test.go
git commit -m "feat: add session metadata and progress tracking types"
```

---

### Task 2: Progress Tracker Tests

**Files:**
- Modify: `transfer/session_test.go`

- [ ] **Step 1: Write failing tests for ProgressTracker**

Append to `transfer/session_test.go`:

```go
func TestProgressTracker_MarkAndCheck(t *testing.T) {
	dir := t.TempDir()
	pt := NewProgressTracker(dir, "sess1")

	if pt.IsComplete(0, "a.mp4") {
		t.Error("should not be complete before marking")
	}

	pt.MarkComplete(0, "a.mp4")

	if !pt.IsComplete(0, "a.mp4") {
		t.Error("should be complete after marking")
	}
	if pt.IsComplete(0, "b.mp4") {
		t.Error("b.mp4 should not be complete")
	}
	if pt.IsComplete(1, "a.mp4") {
		t.Error("card 1 a.mp4 should not be complete")
	}
}

func TestProgressTracker_PersistAndReload(t *testing.T) {
	dir := t.TempDir()
	pt := NewProgressTracker(dir, "sess1")
	pt.MarkComplete(0, "a.mp4")
	pt.MarkComplete(0, "b.mp4")
	pt.MarkComplete(1, "c.mov")

	// Create a new tracker pointing at the same dir — should load state
	pt2 := NewProgressTracker(dir, "sess1")
	if !pt2.IsComplete(0, "a.mp4") {
		t.Error("reloaded tracker should have a.mp4 complete")
	}
	if !pt2.IsComplete(1, "c.mov") {
		t.Error("reloaded tracker should have c.mov complete")
	}
}

func TestProgressTracker_IgnoresWrongSession(t *testing.T) {
	dir := t.TempDir()
	pt := NewProgressTracker(dir, "sess1")
	pt.MarkComplete(0, "a.mp4")

	// New tracker with different session ID should NOT load old data
	pt2 := NewProgressTracker(dir, "sess2")
	if pt2.IsComplete(0, "a.mp4") {
		t.Error("tracker with different session should not load old progress")
	}
}

func TestWriteAndReadDumpMetadata(t *testing.T) {
	dir := t.TempDir()
	meta := DumpMetadata{
		SessionID: "test123",
		Role:      "source",
		CardIndex: 0,
		CardName:  "card-1-TEST",
		StartedAt: "2026-04-04T00:00:00Z",
	}
	if err := WriteDumpMetadata(dir, meta); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadDumpMetadata(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SessionID != "test123" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "test123")
	}
	if got.CardName != "card-1-TEST" {
		t.Errorf("CardName = %q, want %q", got.CardName, "card-1-TEST")
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./transfer/ -run "TestProgressTracker|TestWriteAndRead" -v`
Expected: PASS (implementation already exists from Task 1)

- [ ] **Step 3: Commit**

```bash
git add transfer/session_test.go
git commit -m "test: add progress tracker and metadata round-trip tests"
```

---

### Task 3: Volume Scanner

**Files:**
- Create: `transfer/volumes.go`
- Test: `transfer/volumes_test.go`

- [ ] **Step 1: Write failing tests for volume scanning**

```go
// transfer/volumes_test.go
package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindVolumeBySession_Source(t *testing.T) {
	// Create fake volume directories with dump.json
	root := t.TempDir()
	vol1 := filepath.Join(root, "CARD_A")
	vol2 := filepath.Join(root, "CARD_B")
	os.MkdirAll(vol1, 0755)
	os.MkdirAll(vol2, 0755)

	WriteDumpMetadata(vol1, DumpMetadata{
		SessionID: "sess1",
		Role:      "source",
		CardIndex: 0,
	})
	WriteDumpMetadata(vol2, DumpMetadata{
		SessionID: "sess1",
		Role:      "source",
		CardIndex: 1,
	})

	mount, found := FindVolumeBySession(root, "sess1", "source", 0)
	if !found {
		t.Fatal("expected to find volume for card 0")
	}
	if mount != vol1 {
		t.Errorf("mount = %q, want %q", mount, vol1)
	}

	mount, found = FindVolumeBySession(root, "sess1", "source", 1)
	if !found {
		t.Fatal("expected to find volume for card 1")
	}
	if mount != vol2 {
		t.Errorf("mount = %q, want %q", mount, vol2)
	}
}

func TestFindVolumeBySession_Destination(t *testing.T) {
	root := t.TempDir()
	vol := filepath.Join(root, "SSD")
	os.MkdirAll(vol, 0755)

	WriteDumpMetadata(vol, DumpMetadata{
		SessionID: "sess1",
		Role:      "destination",
	})

	mount, found := FindVolumeBySession(root, "sess1", "destination", -1)
	if !found {
		t.Fatal("expected to find destination volume")
	}
	if mount != vol {
		t.Errorf("mount = %q, want %q", mount, vol)
	}
}

func TestFindVolumeBySession_NotFound(t *testing.T) {
	root := t.TempDir()
	vol := filepath.Join(root, "CARD")
	os.MkdirAll(vol, 0755)

	WriteDumpMetadata(vol, DumpMetadata{
		SessionID: "other",
		Role:      "source",
		CardIndex: 0,
	})

	_, found := FindVolumeBySession(root, "sess1", "source", 0)
	if found {
		t.Error("should not find volume for wrong session")
	}
}

func TestFindAllSessionVolumes(t *testing.T) {
	root := t.TempDir()
	vol1 := filepath.Join(root, "CARD_A")
	vol2 := filepath.Join(root, "SSD")
	vol3 := filepath.Join(root, "UNRELATED")
	os.MkdirAll(vol1, 0755)
	os.MkdirAll(vol2, 0755)
	os.MkdirAll(vol3, 0755)

	WriteDumpMetadata(vol1, DumpMetadata{SessionID: "sess1", Role: "source", CardIndex: 0})
	WriteDumpMetadata(vol2, DumpMetadata{SessionID: "sess1", Role: "destination"})
	WriteDumpMetadata(vol3, DumpMetadata{SessionID: "other", Role: "source", CardIndex: 0})

	results := FindAllSessionVolumes(root, "sess1")
	if len(results) != 2 {
		t.Fatalf("found %d volumes, want 2", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./transfer/ -run "TestFindVolume|TestFindAllSession" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Implement volume scanner**

```go
// transfer/volumes.go
package transfer

import (
	"os"
	"path/filepath"
)

// VolumesRoot is the directory where external volumes are mounted.
// Overridable for testing.
var VolumesRoot = "/Volumes"

// VolumeMatch represents a drive that matched a session scan.
type VolumeMatch struct {
	MountPoint string
	Meta       DumpMetadata
}

// FindVolumeBySession scans directories under scanRoot for a dump.json matching
// the given session ID, role, and card index. For role "destination", cardIndex
// is ignored. Returns the mount point and true if found.
func FindVolumeBySession(scanRoot, sessionID, role string, cardIndex int) (string, bool) {
	entries, err := os.ReadDir(scanRoot)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mountPoint := filepath.Join(scanRoot, entry.Name())
		meta, err := ReadDumpMetadata(mountPoint)
		if err != nil {
			continue
		}
		if meta.SessionID != sessionID {
			continue
		}
		if meta.Role != role {
			continue
		}
		if role == "source" && meta.CardIndex != cardIndex {
			continue
		}
		return mountPoint, true
	}
	return "", false
}

// FindAllSessionVolumes scans directories under scanRoot and returns all drives
// that have a dump.json matching the given session ID.
func FindAllSessionVolumes(scanRoot, sessionID string) []VolumeMatch {
	entries, err := os.ReadDir(scanRoot)
	if err != nil {
		return nil
	}
	var matches []VolumeMatch
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mountPoint := filepath.Join(scanRoot, entry.Name())
		meta, err := ReadDumpMetadata(mountPoint)
		if err != nil {
			continue
		}
		if meta.SessionID == sessionID {
			matches = append(matches, VolumeMatch{MountPoint: mountPoint, Meta: meta})
		}
	}
	return matches
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./transfer/ -run "TestFindVolume|TestFindAllSession" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add transfer/volumes.go transfer/volumes_test.go
git commit -m "feat: add volume scanner that finds drives by dump.json identity"
```

---

### Task 4: Engine — Context Support and Metadata Writing

**Files:**
- Modify: `transfer/engine.go`
- Modify: `transfer/engine_test.go`

- [ ] **Step 1: Write failing test for engine session metadata**

Append to `transfer/engine_test.go`:

```go
import (
	"context"
	"encoding/json"
	// existing imports...
)

func TestEngine_WritesMetadata(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "test.mp4"), []byte("video"), 0644)

	cards := []CardSource{
		{MountPoint: srcDir, VolumeName: "TestCard", CardIndex: 0},
	}

	e, err := NewEngine(context.Background(), cards, destDir, 2, 3)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if e.SessionID == "" {
		t.Error("SessionID should not be empty")
	}

	// Check source dump.json
	srcMeta, err := ReadDumpMetadata(srcDir)
	if err != nil {
		t.Fatalf("read source metadata: %v", err)
	}
	if srcMeta.Role != "source" {
		t.Errorf("source role = %q, want %q", srcMeta.Role, "source")
	}
	if srcMeta.SessionID != e.SessionID {
		t.Errorf("source session = %q, want %q", srcMeta.SessionID, e.SessionID)
	}

	// Check dest dump.json
	destMeta, err := ReadDumpMetadata(destDir)
	if err != nil {
		t.Fatalf("read dest metadata: %v", err)
	}
	if destMeta.Role != "destination" {
		t.Errorf("dest role = %q, want %q", destMeta.Role, "destination")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./transfer/ -run TestEngine_WritesMetadata -v`
Expected: FAIL — `NewEngine` doesn't accept `context.Context` yet

- [ ] **Step 3: Update Engine to accept context and write metadata**

Modify `transfer/engine.go`:

Change the `Engine` struct to add `SessionID`, `ctx`, and `Progress` fields:

```go
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
```

Add `"context"` and `"time"` to the imports.

Replace `NewEngine` function:

```go
func NewEngine(ctx context.Context, cards []CardSource, destBase string, maxConcurrent, maxRetries int) (*Engine, error) {
	sessionID := GenerateSessionID()

	e := &Engine{
		DestBase:      destBase,
		MaxConcurrent: maxConcurrent,
		MaxRetries:    maxRetries,
		SessionID:     sessionID,
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

	// Write dump.json to each source
	now := time.Now().UTC().Format(time.RFC3339)
	for _, card := range cards {
		WriteDumpMetadata(card.MountPoint, DumpMetadata{
			SessionID: sessionID,
			Role:      "source",
			CardIndex: card.CardIndex,
			CardName:  fmt.Sprintf("card-%d-%s", card.CardIndex+1, card.VolumeName),
			StartedAt: now,
		})
	}

	// Write dump.json to destination
	sourceIDs := make([]int, len(cards))
	for i, c := range cards {
		sourceIDs[i] = c.CardIndex
	}
	WriteDumpMetadata(destBase, DumpMetadata{
		SessionID:     sessionID,
		Role:          "destination",
		SourceCardIDs: sourceIDs,
		StartedAt:     now,
	})

	// Initialize progress tracker
	e.progress = NewProgressTracker(destBase, sessionID)

	return e, nil
}
```

- [ ] **Step 4: Update existing TestEngineCreation to pass context**

In `transfer/engine_test.go`, update `TestEngineCreation`:

```go
func TestEngineCreation(t *testing.T) {
	tmpDir := t.TempDir()

	cards := []CardSource{
		{MountPoint: tmpDir, VolumeName: "TestCard", CardIndex: 0},
	}

	// Create a media file
	os.WriteFile(tmpDir+"/test.mp4", []byte("video"), 0644)

	e, err := NewEngine(context.Background(), cards, t.TempDir(), 2, 3)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if e.MaxConcurrent != 2 {
		t.Errorf("MaxConcurrent = %d, want 2", e.MaxConcurrent)
	}
	if e.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", e.MaxRetries)
	}
	if len(e.Cards) != 1 {
		t.Fatalf("Cards = %d, want 1", len(e.Cards))
	}
	if e.Cards[0].TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1", e.Cards[0].TotalFiles)
	}
}
```

- [ ] **Step 5: Update model.go call site to pass context**

In `model.go`, update `startTransfer()` to pass `context.Background()` to `NewEngine`:

Find:
```go
engine, err := transfer.NewEngine(cards, m.destPath, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
```
Replace with:
```go
engine, err := transfer.NewEngine(context.Background(), cards, m.destPath, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
```

Add `"context"` to the imports in `model.go`.

- [ ] **Step 6: Run all tests to verify everything passes**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add transfer/engine.go transfer/engine_test.go model.go
git commit -m "feat: engine accepts context, writes dump.json to all drives"
```

---

### Task 5: Engine — Reconnection Wait Loop

**Files:**
- Modify: `transfer/engine.go`
- Modify: `transfer/engine_test.go`

- [ ] **Step 1: Add new event types**

In `transfer/engine.go`, add two new event types to the const block:

```go
const (
	EventFileStart EventType = iota
	EventFileProgress
	EventFileComplete
	EventFileRetry
	EventFileFailed
	EventCardPaused
	EventCardWaiting
	EventCardResumed
	EventAllComplete
)
```

- [ ] **Step 2: Write failing test for waitForVolume**

Append to `transfer/engine_test.go`:

```go
func TestEngine_WaitForVolume_AlreadyPresent(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "test.mp4"), []byte("video"), 0644)

	ctx := context.Background()
	cards := []CardSource{
		{MountPoint: srcDir, VolumeName: "TestCard", CardIndex: 0},
	}

	e, err := NewEngine(ctx, cards, destDir, 2, 3)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Source dump.json already written by NewEngine, volume is present
	// waitForVolume should return immediately with the same mount point
	scanRoot := filepath.Dir(srcDir)
	mount, err := e.waitForVolume(scanRoot, "source", 0)
	if err != nil {
		t.Fatalf("waitForVolume: %v", err)
	}
	if mount != srcDir {
		t.Errorf("mount = %q, want %q", mount, srcDir)
	}
}

func TestEngine_WaitForVolume_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	e := &Engine{
		SessionID: "nosuchsession",
		ctx:       ctx,
	}

	_, err := e.waitForVolume(t.TempDir(), "source", 0)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./transfer/ -run "TestEngine_WaitForVolume" -v`
Expected: FAIL — `waitForVolume` not defined

- [ ] **Step 4: Implement waitForVolume**

Add to `transfer/engine.go`:

```go
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./transfer/ -run "TestEngine_WaitForVolume" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add transfer/engine.go transfer/engine_test.go
git commit -m "feat: add waitForVolume with indefinite polling and context cancellation"
```

---

### Task 6: Engine — Replace Retry Loop with Reconnection Logic

**Files:**
- Modify: `transfer/engine.go`

- [ ] **Step 1: Replace processJob with reconnection-aware version**

Replace the entire `processJob` method in `transfer/engine.go`:

```go
func (e *Engine) processJob(j *TransferJob) {
	card := &e.Cards[j.CardIndex]
	scanRoot := VolumesRoot

	// Check/wait for source volume
	newMount, err := e.ensureVolume(scanRoot, "source", j.CardIndex, card.VolumeName)
	if err != nil {
		return // context cancelled
	}
	if newMount != card.MountPoint {
		// Drive remounted at different path — update references
		oldMount := card.MountPoint
		card.MountPoint = newMount
		j.File.AbsPath = strings.Replace(j.File.AbsPath, oldMount, newMount, 1)
	}

	// Check/wait for destination volume
	_, err = e.ensureVolume(scanRoot, "destination", -1, "destination")
	if err != nil {
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

	for attempt := 0; attempt <= e.MaxRetries; attempt++ {
		if attempt > 0 {
			// Check if this is a disconnection — if so, wait for reconnect
			if VolumeMissing(card.MountPoint) || VolumeMissing(e.DestBase) {
				// Wait for source
				newMount, err := e.ensureVolume(scanRoot, "source", j.CardIndex, card.VolumeName)
				if err != nil {
					return
				}
				if newMount != card.MountPoint {
					oldMount := card.MountPoint
					card.MountPoint = newMount
					j.File.AbsPath = strings.Replace(j.File.AbsPath, oldMount, newMount, 1)
				}
				// Wait for destination
				newDest, err := e.ensureVolume(scanRoot, "destination", -1, "destination")
				if err != nil {
					return
				}
				if newDest != e.DestBase {
					oldDest := e.DestBase
					e.DestBase = newDest
					j.Dest = strings.Replace(j.Dest, oldDest, newDest, 1)
					destDir = filepath.Dir(j.Dest)
					os.MkdirAll(destDir, 0755)
				}
				// Reset attempt counter — disconnection isn't a "real" failure
				attempt = 0
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
		}
	}
}

// ensureVolume checks if a volume is present. If missing, emits a waiting event
// and polls until it reappears or context is cancelled.
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
```

Add `"strings"` to the imports in `engine.go` if not already present.

- [ ] **Step 2: Update Engine.Run() to respect context**

Replace `Engine.Run()`:

```go
func (e *Engine) Run() {
	defer close(e.Events)

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.MaxConcurrent)

	for {
		// Check context before popping next job
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
```

- [ ] **Step 3: Verify build compiles**

Run: `go build ./...`
Expected: Success with no errors

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add transfer/engine.go
git commit -m "feat: replace retry loop with indefinite reconnection wait"
```

---

### Task 7: Dashboard — Waiting and Resumed States

**Files:**
- Modify: `components/dashboard.go`

- [ ] **Step 1: Add Waiting field to CardProgress**

In `components/dashboard.go`, add a `Waiting` field and `WaitingFor` field to `CardProgress`:

```go
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
	WaitingFor     string // e.g. "CANON_EOS"
	Done           bool
}
```

- [ ] **Step 2: Update View() to render waiting state**

In `components/dashboard.go`, update the per-card rendering block. Find:

```go
		} else if c.Paused {
			cardsView.WriteString(logWarnStyle.Render("    ⚠ Volume disconnected") + "\n")
		} else if c.CurrentFile != "" {
```

Replace with:

```go
		} else if c.Waiting {
			cardsView.WriteString(logWarnStyle.Render(fmt.Sprintf("    ⏳ Waiting for %s to reconnect...", c.WaitingFor)) + "\n")
		} else if c.Paused {
			cardsView.WriteString(logWarnStyle.Render("    ⚠ Volume disconnected") + "\n")
		} else if c.CurrentFile != "" {
```

- [ ] **Step 3: Add LogReconnected log type**

Add to the `LogEntryType` const block:

```go
const (
	LogComplete LogEntryType = iota
	LogRetry
	LogFailed
	LogWarning
	LogReconnected
)
```

Update the log rendering in `View()`. Find:

```go
		case LogWarning:
			prefix = logWarnStyle.Render("  ⚠")
		}
```

Replace with:

```go
		case LogWarning:
			prefix = logWarnStyle.Render("  ⚠")
		case LogReconnected:
			prefix = logSuccess.Render("  ↻")
		}
```

- [ ] **Step 4: Verify build compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add components/dashboard.go
git commit -m "feat: dashboard shows waiting/reconnected states for disconnected drives"
```

---

### Task 8: Model — Handle New Event Types and Ctrl+C

**Files:**
- Modify: `model.go`

- [ ] **Step 1: Add session tracking and cancel function to model**

In `model.go`, update the `model` struct to add context management and session state:

```go
type model struct {
	step wizardStep

	// Drive data
	allDrives []DiskInfo

	// Step 1: Source selection
	sourceList components.DriveListModel

	// Step 2: Destination drive selection
	destList     components.DriveListModel
	destIndexMap []int
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
```

Add `"time"` to the imports.

- [ ] **Step 2: Update startTransfer to use cancellable context**

Replace the `startTransfer` method:

```go
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
		m.err = err.Error()
		return m, nil
	}

	m.engine = engine
	m.cancelEngine = cancel
	m.sessionID = engine.SessionID

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
}
```

- [ ] **Step 3: Update Ctrl+C handling in Update()**

Replace the `ctrl+c` case in `Update()`:

```go
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.step == stepTransfer && !m.dashboard.AllDone {
				now := time.Now()
				if !m.lastCtrlC.IsZero() && now.Sub(m.lastCtrlC) < 3*time.Second {
					// Double Ctrl+C — exit with resume code
					m.cancelEngine()
					return m, tea.Sequence(
						tea.Printf("\nTo resume this session: dump --resume %s\n", m.sessionID),
						tea.Quit,
					)
				}
				// First Ctrl+C — cancel engine, go back to home
				m.lastCtrlC = now
				m.cancelEngine()
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
```

- [ ] **Step 4: Handle EventCardWaiting and EventCardResumed in applyTransferEvent**

Add cases to `applyTransferEvent` in `model.go`:

```go
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
```

- [ ] **Step 5: Verify build compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 6: Commit**

```bash
git add model.go
git commit -m "feat: Ctrl+C handling and new event types for reconnection UI"
```

---

### Task 9: Session Cleanup on Completion

**Files:**
- Modify: `model.go`

- [ ] **Step 1: Add cleanup when transfer completes**

In `model.go`, update the `EventAllComplete` handling in `updateTransfer`:

Find:
```go
		if evt.Type == transfer.EventAllComplete {
			m.dashboard.AllDone = true
			return m, nil
		}
```

Replace with:
```go
		if evt.Type == transfer.EventAllComplete {
			m.dashboard.AllDone = true
			// Clean up session metadata from all drives
			if m.engine != nil {
				for _, card := range m.engine.Cards {
					transfer.RemoveDumpMetadata(card.MountPoint)
				}
				transfer.RemoveDumpMetadata(m.engine.DestBase)
				transfer.RemoveProgressFile(m.engine.DestBase)
			}
			return m, nil
		}
```

- [ ] **Step 2: Verify build compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add model.go
git commit -m "feat: clean up dump.json files after successful transfer"
```

---

### Task 10: CLI Resume Flag

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add --resume flag and resume startup path**

Replace `main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mikekenway/sdcard-dump/transfer"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	resumeID := flag.String("resume", "", "Resume a previous session by ID")
	flag.Parse()

	var m model
	if *resumeID != "" {
		m = resumeModel(*resumeID)
	} else {
		m = initialModel()
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Implement resumeModel in model.go**

Add to `model.go`:

```go
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

	m := model{
		step:         stepTransfer,
		engine:       engine,
		cancelEngine: cancel,
		sessionID:    sessionID,
		dashboard:    components.NewDashboard(dashCards),
	}

	return m
}
```

- [ ] **Step 3: Add NewEngineResume to transfer/engine.go**

Add to `transfer/engine.go`:

```go
// NewEngineResume creates an engine that resumes an existing session.
// It uses the existing session ID and skips writing new dump.json files.
// Already-completed files (from dump-progress.json) are skipped during Run().
func NewEngineResume(ctx context.Context, sessionID string, cards []CardSource, destBase string, maxConcurrent, maxRetries int) (*Engine, error) {
	e := &Engine{
		DestBase:      destBase,
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
	return e, nil
}
```

- [ ] **Step 4: Update resumeModel Init to start transfer**

The resume model needs to start the engine. Add a custom Init that kicks off the transfer. In `model.go`, update `Init()`:

```go
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
```

- [ ] **Step 5: Verify build compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add main.go model.go transfer/engine.go
git commit -m "feat: add --resume CLI flag and NewEngineResume for session recovery"
```

---

### Task 11: Home Screen Resume Option

**Files:**
- Modify: `model.go`

- [ ] **Step 1: Add home screen step and resume flow**

Add a new wizard step at the beginning:

```go
const (
	stepHome         wizardStep = iota
	stepSourceSelect
	stepDestSelect
	stepConfirm
	stepResumeSelect
	stepTransfer
)
```

- [ ] **Step 2: Update initialModel to start at stepHome**

In `initialModel()`, change:

```go
return model{
	allDrives:  drives,
	sourceList: components.NewDriveList(driveInfos, true),
	step:       stepHome,
}
```

- [ ] **Step 3: Add home screen update handler**

Add to `model.go`:

```go
func (m model) updateHome(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "1", "n":
			m.step = stepSourceSelect
		case "2", "r":
			m.step = stepResumeSelect
			// Build drive list for resume selection (all external drives)
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
			m.destList = components.NewDriveList(resumeDrives, true) // multi-select
		}
	}
	return m, nil
}
```

- [ ] **Step 4: Add resume select handler**

Add to `model.go`:

```go
type resumeReadyMsg struct {
	engine    *transfer.Engine
	cancel    context.CancelFunc
	sessionID string
	dashCards []components.CardProgress
	err       string
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
			m.step = stepHome
			return m, nil
		}

		// Use the first (or only) session found
		var sessionID string
		var matches []transfer.VolumeMatch
		for id, m := range sessions {
			sessionID = id
			matches = m
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
			m.step = stepHome
			return m, nil
		}
		if len(sources) == 0 {
			m.err = "No source drives found in selection"
			m.step = stepHome
			return m, nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		engine, err := transfer.NewEngineResume(ctx, sessionID, sources, destPath, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
		if err != nil {
			cancel()
			m.err = fmt.Sprintf("Resume failed: %v", err)
			m.step = stepHome
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
```

- [ ] **Step 5: Route new steps in Update()**

Update the step routing in `Update()`:

```go
	switch m.step {
	case stepHome:
		return m.updateHome(msg)
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
```

- [ ] **Step 6: Update handleBack for new steps**

```go
func (m model) handleBack() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepHome:
		return m, tea.Quit
	case stepSourceSelect:
		m.step = stepHome
	case stepDestSelect:
		m.step = stepSourceSelect
	case stepConfirm:
		m.step = stepDestSelect
	case stepResumeSelect:
		m.step = stepHome
	}
	return m, nil
}
```

- [ ] **Step 7: Update first Ctrl+C to go to stepHome**

In the Ctrl+C handler, change `m.step = stepSourceSelect` to `m.step = stepHome`.

- [ ] **Step 8: Render new screens in View()**

Update `View()` to add the home screen and resume select screen:

```go
	case stepHome:
		b.WriteString(titleStyle.Render("Welcome to Dump!"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("When you just need to take a dump"))
		b.WriteString("\n\n")
		b.WriteString("  " + confirmKey.Render("[1]") + " New Session\n")
		b.WriteString("  " + confirmKey.Render("[2]") + " Resume Session\n")
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Press 1 or 2 to choose • esc: quit"))

	case stepSourceSelect:
		b.WriteString(titleStyle.Render("Step 1/3 — Select Source Cards"))
		b.WriteString("\n")
		b.WriteString(m.sourceList.View())
		b.WriteString(helpStyle.Render("space: toggle • enter: confirm • esc: back"))
```

Remove the "Welcome to Dump!" banner from the `stepSourceSelect` case (it now lives on the home screen).

Add the resume select rendering:

```go
	case stepResumeSelect:
		b.WriteString(titleStyle.Render("Resume Session — Select Drives"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Select all drives that belong to the session"))
		b.WriteString("\n\n")
		b.WriteString(m.destList.View())
		b.WriteString(helpStyle.Render("space: toggle • enter: confirm • esc: back"))
```

- [ ] **Step 9: Verify build compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 10: Commit**

```bash
git add model.go
git commit -m "feat: add home screen with New Session / Resume Session options"
```

---

### Task 12: Integration Test for Resume Flow

**Files:**
- Modify: `transfer/session_test.go`

- [ ] **Step 1: Write integration test for full resume round-trip**

Append to `transfer/session_test.go`:

```go
func TestResumeRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Simulate: engine wrote metadata, transferred some files, then stopped
	sessionID := "testresume"
	WriteDumpMetadata(srcDir, DumpMetadata{
		SessionID: sessionID,
		Role:      "source",
		CardIndex: 0,
		CardName:  "card-1-TEST",
		StartedAt: "2026-04-04T00:00:00Z",
	})
	WriteDumpMetadata(destDir, DumpMetadata{
		SessionID:     sessionID,
		Role:          "destination",
		SourceCardIDs: []int{0},
		StartedAt:     "2026-04-04T00:00:00Z",
	})

	// Mark one file as already completed
	pt := NewProgressTracker(destDir, sessionID)
	pt.MarkComplete(0, "already-done.mp4")

	// Verify the tracker loads correctly on a fresh instance
	pt2 := NewProgressTracker(destDir, sessionID)
	if !pt2.IsComplete(0, "already-done.mp4") {
		t.Error("expected already-done.mp4 to be complete after reload")
	}
	if pt2.IsComplete(0, "not-done.mp4") {
		t.Error("expected not-done.mp4 to NOT be complete")
	}

	// Verify volume scanning finds the drives
	scanRoot := t.TempDir()
	// Move our volumes under scanRoot for scanning
	vol1 := filepath.Join(scanRoot, "SRC")
	vol2 := filepath.Join(scanRoot, "DST")
	os.MkdirAll(vol1, 0755)
	os.MkdirAll(vol2, 0755)

	WriteDumpMetadata(vol1, DumpMetadata{SessionID: sessionID, Role: "source", CardIndex: 0})
	WriteDumpMetadata(vol2, DumpMetadata{SessionID: sessionID, Role: "destination"})

	matches := FindAllSessionVolumes(scanRoot, sessionID)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	// Verify we can find specific roles
	srcMount, found := FindVolumeBySession(scanRoot, sessionID, "source", 0)
	if !found {
		t.Fatal("expected to find source volume")
	}
	if srcMount != vol1 {
		t.Errorf("source mount = %q, want %q", srcMount, vol1)
	}

	destMount, found := FindVolumeBySession(scanRoot, sessionID, "destination", -1)
	if !found {
		t.Fatal("expected to find destination volume")
	}
	if destMount != vol2 {
		t.Errorf("dest mount = %q, want %q", destMount, vol2)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./transfer/ -run TestResumeRoundTrip -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add transfer/session_test.go
git commit -m "test: add integration test for full resume round-trip"
```

---

### Task 13: Final Build Verification and Cleanup

**Files:**
- All modified files

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 2: Build the binary**

Run: `make build`
Expected: Clean build, binary at `bin/dump`

- [ ] **Step 3: Verify --resume flag works**

Run: `./bin/dump --help`
Expected: Shows `-resume` flag in usage

Run: `./bin/dump --resume nonexistent`
Expected: Shows error "No drives found for session nonexistent"

- [ ] **Step 4: Commit any final fixes**

```bash
git add -A
git commit -m "chore: final cleanup for reconnection and resume feature"
```

Plan complete and saved to `docs/superpowers/plans/2026-04-04-reconnection-resume-plan.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?