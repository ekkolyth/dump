# SD Card Dump CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an interactive Go TUI that discovers mounted volumes, lets the user select source cards and a destination folder, then copies media files using rsync with concurrency control, retry logic, and a real-time progress dashboard.

**Architecture:** Hybrid Bubble Tea app — single top-level model with state machine for wizard flow, delegating to standalone components (drive list, file browser, dashboard). Transfer engine runs in goroutines, communicating via Bubble Tea messages. Drive discovery via `diskutil` plist parsing. File transfer via `rsync --partial --progress --checksum` subprocesses.

**Tech Stack:** Go, Bubble Tea v1 (`github.com/charmbracelet/bubbletea`), Lip Gloss v1 (`github.com/charmbracelet/lipgloss`), `howett.net/plist` for diskutil parsing, `rsync` system binary.

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `main.go` (placeholder)

- [ ] **Step 1: Initialize Go module and install dependencies**

```bash
cd /Users/mikekenway/Development/sdcard-dump
go mod init github.com/mikekenway/sdcard-dump
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/lipgloss@latest
go get howett.net/plist@latest
```

- [ ] **Step 2: Create placeholder main.go**

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("sdcard-dump")
}
```

- [ ] **Step 3: Verify it builds**

Run: `go build -o sdcard-dump .`
Expected: builds without errors, produces `sdcard-dump` binary.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum main.go
git commit -m "Initialize Go project with dependencies"
```

---

### Task 2: Drive Discovery — Plist Parsing

**Files:**
- Create: `drives.go`
- Create: `drives_test.go`

- [ ] **Step 1: Write test for parsing diskutil info plist**

Create `drives_test.go`:

```go
package main

import (
	"testing"

	"howett.net/plist"
)

const testDiskInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>DeviceIdentifier</key>
	<string>disk4s1</string>
	<key>VolumeName</key>
	<string>Untitled</string>
	<key>MountPoint</key>
	<string>/Volumes/Untitled</string>
	<key>TotalSize</key>
	<integer>127800000000</integer>
	<key>FreeSpace</key>
	<integer>50000000000</integer>
	<key>FilesystemType</key>
	<string>ntfs</string>
	<key>FilesystemName</key>
	<string>Windows_NTFS</string>
	<key>Internal</key>
	<false/>
	<key>Removable</key>
	<true/>
	<key>RemovableMediaOrExternalDevice</key>
	<true/>
	<key>Ejectable</key>
	<true/>
	<key>BusProtocol</key>
	<string>USB</string>
	<key>WholeDisk</key>
	<false/>
	<key>APFSContainerFree</key>
	<integer>0</integer>
</dict>
</plist>`

func TestParseDiskInfo(t *testing.T) {
	var info DiskInfo
	if _, err := plist.Unmarshal([]byte(testDiskInfoPlist), &info); err != nil {
		t.Fatalf("failed to parse plist: %v", err)
	}

	if info.DeviceIdentifier != "disk4s1" {
		t.Errorf("DeviceIdentifier = %q, want %q", info.DeviceIdentifier, "disk4s1")
	}
	if info.VolumeName != "Untitled" {
		t.Errorf("VolumeName = %q, want %q", info.VolumeName, "Untitled")
	}
	if info.MountPoint != "/Volumes/Untitled" {
		t.Errorf("MountPoint = %q, want %q", info.MountPoint, "/Volumes/Untitled")
	}
	if info.TotalSize != 127800000000 {
		t.Errorf("TotalSize = %d, want %d", info.TotalSize, 127800000000)
	}
	if info.FreeSpace != 50000000000 {
		t.Errorf("FreeSpace = %d, want %d", info.FreeSpace, 50000000000)
	}
	if info.FilesystemName != "Windows_NTFS" {
		t.Errorf("FilesystemName = %q, want %q", info.FilesystemName, "Windows_NTFS")
	}
	if info.Internal {
		t.Error("Internal = true, want false")
	}
	if !info.IsExternal() {
		t.Error("IsExternal() = false, want true")
	}
}

const testAPFSDiskInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>DeviceIdentifier</key>
	<string>disk3s5</string>
	<key>VolumeName</key>
	<string>Data</string>
	<key>MountPoint</key>
	<string>/System/Volumes/Data</string>
	<key>TotalSize</key>
	<integer>994700000000</integer>
	<key>FreeSpace</key>
	<integer>0</integer>
	<key>APFSContainerFree</key>
	<integer>75000000000</integer>
	<key>FilesystemType</key>
	<string>apfs</string>
	<key>FilesystemName</key>
	<string>APFS</string>
	<key>Internal</key>
	<true/>
	<key>Removable</key>
	<false/>
	<key>RemovableMediaOrExternalDevice</key>
	<false/>
	<key>Ejectable</key>
	<false/>
	<key>BusProtocol</key>
	<string>Apple Fabric</string>
	<key>WholeDisk</key>
	<false/>
</dict>
</plist>`

func TestParseDiskInfoAPFS(t *testing.T) {
	var info DiskInfo
	if _, err := plist.Unmarshal([]byte(testAPFSDiskInfoPlist), &info); err != nil {
		t.Fatalf("failed to parse plist: %v", err)
	}

	if info.EffectiveFreeSpace() != 75000000000 {
		t.Errorf("EffectiveFreeSpace() = %d, want %d", info.EffectiveFreeSpace(), 75000000000)
	}
	if info.IsExternal() {
		t.Error("IsExternal() = true, want false for internal APFS")
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{127800000000, "119.0 GB"},
	}
	for _, tt := range tests {
		got := FormatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestParseDiskInfo -v .`
Expected: FAIL — `DiskInfo` type not defined.

- [ ] **Step 3: Implement DiskInfo struct and helpers**

Create `drives.go`:

```go
package main

import (
	"fmt"
	"os/exec"
	"sort"

	"howett.net/plist"
)

// DiskInfo represents parsed output of `diskutil info -plist <disk>`.
type DiskInfo struct {
	DeviceIdentifier               string `plist:"DeviceIdentifier"`
	VolumeName                     string `plist:"VolumeName"`
	MountPoint                     string `plist:"MountPoint"`
	TotalSize                      int64  `plist:"TotalSize"`
	FreeSpace                      int64  `plist:"FreeSpace"`
	APFSContainerFree              int64  `plist:"APFSContainerFree"`
	FilesystemType                 string `plist:"FilesystemType"`
	FilesystemName                 string `plist:"FilesystemName"`
	Internal                       bool   `plist:"Internal"`
	Removable                      bool   `plist:"Removable"`
	RemovableMediaOrExternalDevice bool   `plist:"RemovableMediaOrExternalDevice"`
	Ejectable                      bool   `plist:"Ejectable"`
	BusProtocol                    string `plist:"BusProtocol"`
	WholeDisk                      bool   `plist:"WholeDisk"`
}

func (d *DiskInfo) IsExternal() bool {
	return !d.Internal || d.RemovableMediaOrExternalDevice
}

func (d *DiskInfo) EffectiveFreeSpace() int64 {
	if d.APFSContainerFree > 0 {
		return d.APFSContainerFree
	}
	return d.FreeSpace
}

// DiskutilList represents parsed output of `diskutil list -plist`.
type DiskutilList struct {
	AllDisks  []string `plist:"AllDisks"`
	WholeDisks []string `plist:"WholeDisks"`
}

// FormatSize returns a human-readable size string.
func FormatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	size := float64(bytes)
	for _, unit := range units {
		size /= 1024
		if size < 1024 || unit == "TB" {
			return fmt.Sprintf("%.1f %s", size, unit)
		}
	}
	return fmt.Sprintf("%d B", bytes)
}

// DiscoverDrives finds all mounted volumes via diskutil.
// Returns them sorted: external drives first, then internal.
func DiscoverDrives() ([]DiskInfo, error) {
	listOut, err := exec.Command("diskutil", "list", "-plist").Output()
	if err != nil {
		return nil, fmt.Errorf("diskutil list: %w", err)
	}

	var list DiskutilList
	if _, err := plist.Unmarshal(listOut, &list); err != nil {
		return nil, fmt.Errorf("parse diskutil list: %w", err)
	}

	var drives []DiskInfo
	for _, diskID := range list.AllDisks {
		infoOut, err := exec.Command("diskutil", "info", "-plist", diskID).Output()
		if err != nil {
			continue
		}

		var info DiskInfo
		if _, err := plist.Unmarshal(infoOut, &info); err != nil {
			continue
		}

		// Skip whole disks and unmounted volumes
		if info.WholeDisk || info.MountPoint == "" {
			continue
		}

		drives = append(drives, info)
	}

	// Sort: external first, then by volume name
	sort.Slice(drives, func(i, j int) bool {
		if drives[i].IsExternal() != drives[j].IsExternal() {
			return drives[i].IsExternal()
		}
		return drives[i].VolumeName < drives[j].VolumeName
	})

	return drives, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v .`
Expected: all 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add drives.go drives_test.go
git commit -m "Add drive discovery via diskutil plist parsing"
```

---

### Task 3: rsync Wrapper — Progress Parsing

**Files:**
- Create: `transfer/rsync.go`
- Create: `transfer/rsync_test.go`

- [ ] **Step 1: Write test for rsync progress line parsing**

Create `transfer/rsync_test.go`:

```go
package transfer

import "testing"

func TestParseProgressLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantOK  bool
		wantPct int
		wantSpd string
		wantFin bool
	}{
		{
			name:    "incremental progress",
			line:    "     262144  51%   63.97KB/s   00:00:03",
			wantOK:  true,
			wantPct: 51,
			wantSpd: "63.97KB/s",
			wantFin: false,
		},
		{
			name:    "final line with xfer info",
			line:    "     512000 100%   49.97KB/s   00:00:10 (xfer#1, to-check=0/1)",
			wantOK:  true,
			wantPct: 100,
			wantSpd: "49.97KB/s",
			wantFin: true,
		},
		{
			name:    "large file progress",
			line:    "  104857600  25%    2.10MB/s   00:01:30",
			wantOK:  true,
			wantPct: 25,
			wantSpd: "2.10MB/s",
			wantFin: false,
		},
		{
			name:   "filename line",
			line:   "GX010047.MP4",
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := ParseProgressLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if p.Percentage != tt.wantPct {
				t.Errorf("Percentage = %d, want %d", p.Percentage, tt.wantPct)
			}
			if p.Speed != tt.wantSpd {
				t.Errorf("Speed = %q, want %q", p.Speed, tt.wantSpd)
			}
			if p.IsFinal != tt.wantFin {
				t.Errorf("IsFinal = %v, want %v", p.IsFinal, tt.wantFin)
			}
		})
	}
}

func TestScanCRLF(t *testing.T) {
	// Test with \r-delimited data (rsync progress style)
	data := []byte("line1\rline2\nline3\r")

	var tokens []string
	offset := 0
	for offset < len(data) {
		advance, token, _ := ScanCRLF(data[offset:], false)
		if advance == 0 {
			break
		}
		if token != nil {
			tokens = append(tokens, string(token))
		}
		offset += advance
	}

	if len(tokens) != 3 {
		t.Fatalf("got %d tokens, want 3: %v", len(tokens), tokens)
	}
	expected := []string{"line1", "line2", "line3"}
	for i, want := range expected {
		if tokens[i] != want {
			t.Errorf("token[%d] = %q, want %q", i, tokens[i], want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./transfer/`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement rsync progress parser and runner**

Create `transfer/rsync.go`:

```go
package transfer

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Progress represents a single rsync progress update.
type Progress struct {
	Filename         string
	BytesTransferred int64
	Percentage       int
	Speed            string
	ETA              string
	IsFinal          bool
}

var progressRe = regexp.MustCompile(
	`^\s*(\d+)\s+(\d+)%\s+(\S+/s)\s+(\d+:\d+:\d+)(?:\s+\(xfer#\d+,\s+to-check=\d+/\d+\))?`,
)

var xferRe = regexp.MustCompile(`\(xfer#\d+`)

// ParseProgressLine parses a single rsync --progress output line.
func ParseProgressLine(line string) (Progress, bool) {
	trimmed := strings.TrimSpace(line)
	m := progressRe.FindStringSubmatch(trimmed)
	if m == nil {
		return Progress{}, false
	}

	bytes, _ := strconv.ParseInt(m[1], 10, 64)
	pct, _ := strconv.Atoi(m[2])

	p := Progress{
		BytesTransferred: bytes,
		Percentage:       pct,
		Speed:            m[3],
		ETA:              m[4],
		IsFinal:          xferRe.MatchString(trimmed),
	}

	return p, true
}

// ScanCRLF is a bufio.SplitFunc that splits on \r or \n.
// This is needed because rsync uses \r for incremental progress updates.
func ScanCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\r' || b == '\n' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// RsyncFile copies a single file using rsync with progress reporting.
// onProgress is called for each progress update.
// Returns nil on success, error on failure.
func RsyncFile(src, dst string, onProgress func(Progress)) error {
	args := []string{
		"--partial",
		"--progress",
		"--checksum",
		src,
		dst,
	}

	cmd := exec.Command("rsync", args...)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start rsync: %w", err)
	}

	currentFile := ""
	scanner := bufio.NewScanner(stdout)
	scanner.Split(ScanCRLF)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		p, ok := ParseProgressLine(line)
		if ok {
			p.Filename = currentFile
			if onProgress != nil {
				onProgress(p)
			}
		} else {
			currentFile = strings.TrimSpace(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("rsync exited %d: %s",
				exitErr.ExitCode(), strings.TrimSpace(stderrBuf.String()))
		}
		return fmt.Errorf("rsync: %w", err)
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./transfer/`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add transfer/
git commit -m "Add rsync wrapper with progress line parsing"
```

---

### Task 4: Media File Discovery

**Files:**
- Create: `transfer/discovery.go`
- Create: `transfer/discovery_test.go`

- [ ] **Step 1: Write test for media extension matching and file walking**

Create `transfer/discovery_test.go`:

```go
package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsMediaFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"video.mp4", true},
		{"photo.jpg", true},
		{"photo.JPEG", true},
		{"raw.CR3", true},
		{"raw.arw", true},
		{"video.MOV", true},
		{"audio.wav", true},
		{"pro.braw", true},
		{"readme.txt", false},
		{"database.db", false},
		{"noext", false},
		{".hidden", false},
		{"video.mp4.bak", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMediaFile(tt.name)
			if got != tt.want {
				t.Errorf("IsMediaFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestDiscoverMediaFiles(t *testing.T) {
	// Create a temp directory simulating an SD card
	root := t.TempDir()

	// Create directory structure
	dcim := filepath.Join(root, "DCIM", "100GOPRO")
	os.MkdirAll(dcim, 0755)
	os.MkdirAll(filepath.Join(root, "MISC"), 0755)

	// Create media files
	os.WriteFile(filepath.Join(dcim, "GX010001.MP4"), []byte("video"), 0644)
	os.WriteFile(filepath.Join(dcim, "GX010002.MP4"), []byte("video2"), 0644)
	os.WriteFile(filepath.Join(root, "PICT0001.jpg"), []byte("photo"), 0644)

	// Create non-media files (should be skipped)
	os.WriteFile(filepath.Join(root, "readme.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(root, "MISC", "log.bin"), []byte("bin"), 0644)

	// Hidden dirs should be skipped
	hidden := filepath.Join(root, ".Spotlight-V100")
	os.MkdirAll(hidden, 0755)
	os.WriteFile(filepath.Join(hidden, "store.jpg"), []byte("hidden"), 0644)

	files, err := DiscoverMediaFiles(root)
	if err != nil {
		t.Fatalf("DiscoverMediaFiles: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("got %d files, want 3: %v", len(files), files)
	}

	// Check that relative paths are preserved
	relPaths := make(map[string]bool)
	for _, f := range files {
		relPaths[f.RelPath] = true
	}

	want := []string{
		"DCIM/100GOPRO/GX010001.MP4",
		"DCIM/100GOPRO/GX010002.MP4",
		"PICT0001.jpg",
	}
	for _, w := range want {
		if !relPaths[w] {
			t.Errorf("missing file %q in results", w)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestIsMediaFile -v ./transfer/`
Expected: FAIL — `IsMediaFile` not defined.

- [ ] **Step 3: Implement media file discovery**

Create `transfer/discovery.go`:

```go
package transfer

import (
	"os"
	"path/filepath"
	"strings"
)

// MediaExtensions is the list of file extensions to copy.
var MediaExtensions = map[string]bool{
	// Video
	".mp4": true, ".mov": true, ".avi": true, ".mts": true,
	".m2ts": true, ".mkv": true, ".wmv": true, ".3gp": true,
	// Photo
	".jpg": true, ".jpeg": true, ".png": true, ".tiff": true,
	".tif": true, ".heic": true, ".heif": true,
	// Raw
	".cr2": true, ".cr3": true, ".arw": true, ".nef": true,
	".dng": true, ".raw": true, ".orf": true, ".rw2": true,
	".raf": true, ".srw": true,
	// Pro
	".braw": true, ".r3d": true, ".prores": true, ".mxf": true,
	// Audio
	".wav": true, ".mp3": true, ".aac": true, ".m4a": true,
}

// MediaFile represents a discovered media file on a source card.
type MediaFile struct {
	AbsPath  string // Full path on disk
	RelPath  string // Path relative to the card root
	Size     int64  // File size in bytes
}

// IsMediaFile returns true if the filename has a recognized media extension.
func IsMediaFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return MediaExtensions[ext]
}

// DiscoverMediaFiles walks a directory tree and returns all media files.
// Hidden directories (starting with .) are skipped.
func DiscoverMediaFiles(root string) ([]MediaFile, error) {
	var files []MediaFile

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip files we can't read
		}

		// Skip hidden directories
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && path != root {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		if !IsMediaFile(info.Name()) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		files = append(files, MediaFile{
			AbsPath: path,
			RelPath: rel,
			Size:    info.Size(),
		})

		return nil
	})

	return files, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./transfer/`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add transfer/discovery.go transfer/discovery_test.go
git commit -m "Add media file discovery with extension filtering"
```

---

### Task 5: Transfer Engine

**Files:**
- Create: `transfer/engine.go`
- Create: `transfer/engine_test.go`

- [ ] **Step 1: Write test for transfer engine types and volume-missing detection**

Create `transfer/engine_test.go`:

```go
package transfer

import (
	"os"
	"testing"
)

func TestVolumeMissing(t *testing.T) {
	// Existing path should not be missing
	if VolumeMissing("/") {
		t.Error("VolumeMissing('/') = true, want false")
	}

	// Non-existent path should be missing
	if !VolumeMissing("/Volumes/NoSuchVolume_XYZ_12345") {
		t.Error("VolumeMissing for non-existent path = false, want true")
	}
}

func TestTransferJobQueue(t *testing.T) {
	q := NewJobQueue()

	j1 := &TransferJob{File: MediaFile{RelPath: "a.mp4"}, CardIndex: 0}
	j2 := &TransferJob{File: MediaFile{RelPath: "b.mp4"}, CardIndex: 0}
	j3 := &TransferJob{File: MediaFile{RelPath: "c.mp4"}, CardIndex: 1}

	q.Push(j1)
	q.Push(j2)
	q.Push(j3)

	if q.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", q.Len())
	}

	got := q.Pop()
	if got.File.RelPath != "a.mp4" {
		t.Errorf("Pop() = %q, want %q", got.File.RelPath, "a.mp4")
	}

	if q.Len() != 2 {
		t.Fatalf("Len() after Pop = %d, want 2", q.Len())
	}
}

func TestEngineCreation(t *testing.T) {
	tmpDir := t.TempDir()

	cards := []CardSource{
		{MountPoint: tmpDir, VolumeName: "TestCard", CardIndex: 0},
	}

	// Create a media file
	os.WriteFile(tmpDir+"/test.mp4", []byte("video"), 0644)

	e, err := NewEngine(cards, t.TempDir(), 2, 3)
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

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestVolumeMissing -v ./transfer/`
Expected: FAIL — `VolumeMissing` not defined.

- [ ] **Step 3: Implement transfer engine**

Create `transfer/engine.go`:

```go
package transfer

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MaxConcurrentDefault is the default number of simultaneous rsync processes.
const MaxConcurrentDefault = 2

// MaxRetriesDefault is the default number of retry attempts per file.
const MaxRetriesDefault = 5

// CardSource represents a source card to import from.
type CardSource struct {
	MountPoint string
	VolumeName string
	CardIndex  int
	Files      []MediaFile
	TotalFiles int
	TotalBytes int64
}

// TransferJob represents a single file to transfer.
type TransferJob struct {
	File      MediaFile
	CardIndex int
	Retries   int
	Dest      string // Full destination path
}

// JobQueue is a thread-safe FIFO queue for transfer jobs.
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

// VolumeMissing returns true if the given mount point is no longer accessible.
func VolumeMissing(mountPoint string) bool {
	_, err := os.Stat(mountPoint)
	return os.IsNotExist(err)
}

// TransferEvent is sent from the engine to the UI.
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
	EventCardPaused  // volume disappeared
	EventAllComplete
)

// Engine orchestrates concurrent file transfers.
type Engine struct {
	Cards         []CardSource
	DestBase      string
	MaxConcurrent int
	MaxRetries    int
	Events        chan TransferEvent
	queue         *JobQueue
}

// NewEngine creates an engine, discovering files on each source card.
func NewEngine(cards []CardSource, destBase string, maxConcurrent, maxRetries int) (*Engine, error) {
	e := &Engine{
		DestBase:      destBase,
		MaxConcurrent: maxConcurrent,
		MaxRetries:    maxRetries,
		Events:        make(chan TransferEvent, 100),
		queue:         NewJobQueue(),
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

		// Build destination folder name: card-N-VolumeName
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

// Run starts the transfer. Call this in a goroutine.
// It closes the Events channel when all transfers are complete.
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

	// Check if volume is still accessible
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

	// Ensure destination directory exists
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
			// Check volume before retry
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

			// Exponential backoff
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

		// Last attempt failed
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./transfer/`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add transfer/engine.go transfer/engine_test.go
git commit -m "Add transfer engine with concurrency control and retry logic"
```

---

### Task 6: Drive List Component

**Files:**
- Create: `components/drivelist.go`

- [ ] **Step 1: Implement the drive list component**

Create `components/drivelist.go`:

```go
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
	driveNormal   = lipgloss.NewStyle()
	driveCursor   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	driveSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	driveExtLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	driveIntLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	driveHeader   = lipgloss.NewStyle().Bold(true).Underline(true).MarginBottom(1)
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
			check = "[x] "
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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./components/`
Expected: compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add components/drivelist.go
git commit -m "Add drive list component with multi/single select"
```

---

### Task 7: File Browser Component

**Files:**
- Create: `components/filebrowser.go`

- [ ] **Step 1: Implement the file browser component**

Create `components/filebrowser.go`:

```go
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
			// Select current directory
			return m, func() tea.Msg {
				return FolderSelectedMsg{Path: m.currentPath}
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
	breadcrumb    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).MarginBottom(1)
	dirEntry      = lipgloss.NewStyle().PaddingLeft(2)
	dirCursor     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	createOption  = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Italic(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	inputStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	browserHelp   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./components/`
Expected: compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add components/filebrowser.go
git commit -m "Add file browser component with directory navigation and creation"
```

---

### Task 8: Dashboard Component

**Files:**
- Create: `components/dashboard.go`

- [ ] **Step 1: Implement the dashboard component**

Create `components/dashboard.go`:

```go
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
)

// DashboardModel displays transfer progress and a scrollable log.
type DashboardModel struct {
	Cards      []CardProgress
	Log        []LogEntry
	logOffset  int
	startTime  time.Time
	width      int
	height     int
	AllDone    bool
}

func NewDashboard(cards []CardProgress) DashboardModel {
	return DashboardModel{
		Cards:     cards,
		startTime: time.Now(),
	}
}

// AddLogEntry appends a log entry and auto-scrolls to bottom.
func (m *DashboardModel) AddLogEntry(entryType LogEntryType, message string) {
	m.Log = append(m.Log, LogEntry{
		Time:    time.Now(),
		Type:    entryType,
		Message: message,
	})
	// Auto-scroll to bottom
	maxVisible := m.logVisibleLines()
	if len(m.Log) > maxVisible {
		m.logOffset = len(m.Log) - maxVisible
	}
}

func (m DashboardModel) logVisibleLines() int {
	lines := m.height - (len(m.Cards)*3 + 10) // rough estimate
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
	dashBorder    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(1, 2)
	logBorder     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	progressDone  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	progressTodo  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cardName      = lipgloss.NewStyle().Bold(true)
	speedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	logSuccess    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	logRetryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	logFailStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	logWarnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	summaryStyle  = lipgloss.NewStyle().Bold(true).MarginTop(1)
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

	b.WriteString(dashBorder.Render(title + "\n\n" + cardsView.String()))
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
		}
		logView.WriteString(fmt.Sprintf("%s %s\n", prefix, entry.Message))
	}

	if len(m.Log) == 0 {
		logView.WriteString("  Waiting for transfers...\n")
	}

	b.WriteString(logBorder.Render("Log\n" + logView.String()))

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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./components/`
Expected: compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add components/dashboard.go
git commit -m "Add transfer progress dashboard component"
```

---

### Task 9: Top-Level Model and State Machine

**Files:**
- Create: `model.go`

- [ ] **Step 1: Implement the wizard state machine**

Create `model.go`:

```go
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
	destList    components.DriveListModel
	fileBrowser components.FileBrowserModel
	destPath    string

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
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).MarginBottom(1)
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	confirmKey  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	sectionBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(1, 2)
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
		destList:   components.NewDriveList(driveInfos, false),
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
		// Collect selected sources and advance
		m.selectedSources = nil
		indices := msg.Selected
		sort.Ints(indices)
		for _, i := range indices {
			m.selectedSources = append(m.selectedSources, m.allDrives[i])
		}
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
			driveIdx := msg.Selected[0]
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
		// Calculate summaries for confirmation
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

	// Build dashboard card progress entries
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

	// Start transfer in background, pump events into Bubble Tea
	return m, func() tea.Msg {
		go m.engine.Run()
		// Read first event
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

		// Read next event
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

		// Total
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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build .`
Expected: May fail because `main.go` still has old main. Proceed to next step.

- [ ] **Step 3: Commit**

```bash
git add model.go
git commit -m "Add wizard state machine with all four steps"
```

---

### Task 10: Main Entry Point

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Update main.go to launch the Bubble Tea program**

Replace contents of `main.go`:

```go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build -o sdcard-dump .`
Expected: builds without errors.

- [ ] **Step 3: Run it manually**

Run: `./sdcard-dump`
Expected: TUI launches showing Step 1 with the list of drives. Ctrl+C exits cleanly.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "Wire up main entry point with Bubble Tea program"
```

---

### Task 11: Integration Smoke Test

**Files:**
- Create: `integration_test.go`

- [ ] **Step 1: Write a basic integration test that exercises file discovery + rsync on real files**

Create `integration_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikekenway/sdcard-dump/transfer"
)

func TestIntegration_DiscoverAndTransfer(t *testing.T) {
	// Create a fake SD card structure
	srcDir := t.TempDir()
	dcim := filepath.Join(srcDir, "DCIM", "100GOPRO")
	os.MkdirAll(dcim, 0755)

	// Write a test file
	testData := make([]byte, 1024) // 1KB file
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	srcFile := filepath.Join(dcim, "GX010001.MP4")
	os.WriteFile(srcFile, testData, 0644)

	// Also write a non-media file that should be skipped
	os.WriteFile(filepath.Join(srcDir, "readme.txt"), []byte("skip me"), 0644)

	// Discover files
	files, err := transfer.DiscoverMediaFiles(srcDir)
	if err != nil {
		t.Fatalf("DiscoverMediaFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].RelPath != "DCIM/100GOPRO/GX010001.MP4" {
		t.Errorf("RelPath = %q, want %q", files[0].RelPath, "DCIM/100GOPRO/GX010001.MP4")
	}

	// Transfer the file via rsync
	destDir := t.TempDir()
	destFile := filepath.Join(destDir, "GX010001.MP4")

	var lastProgress transfer.Progress
	err = transfer.RsyncFile(srcFile, destFile, func(p transfer.Progress) {
		lastProgress = p
	})
	if err != nil {
		t.Fatalf("RsyncFile: %v", err)
	}

	// Verify the file was copied correctly
	destData, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(destData) != len(testData) {
		t.Errorf("dest file size = %d, want %d", len(destData), len(testData))
	}
	for i := range testData {
		if destData[i] != testData[i] {
			t.Fatalf("byte mismatch at offset %d", i)
		}
	}

	_ = lastProgress // progress may or may not fire for tiny files
}
```

- [ ] **Step 2: Run integration test**

Run: `go test -v -run TestIntegration .`
Expected: PASS

- [ ] **Step 3: Run all tests**

Run: `go test -v ./...`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add integration_test.go
git commit -m "Add integration smoke test for discovery and rsync transfer"
```

---

### Task 12: Manual End-to-End Test

No new files. This is a manual verification.

- [ ] **Step 1: Build and run with SD cards inserted**

```bash
go build -o sdcard-dump . && ./sdcard-dump
```

- [ ] **Step 2: Walk through the wizard**

1. Verify all drives appear in Step 1, external at top
2. Select source card(s) with Space, press Enter
3. Select destination drive, browse folders with Space, select with Enter
4. Review confirmation screen, press Enter to start
5. Watch dashboard progress
6. Verify files appear at destination in `card-N-VolumeName/` structure

- [ ] **Step 3: Test error recovery**

1. Start a transfer of a large file
2. Eject a source card mid-transfer
3. Verify the dashboard shows a warning and the card is paused
4. Verify other cards continue transferring

- [ ] **Step 4: Fix any issues found, commit**

```bash
git add -A
git commit -m "Fix issues found during manual testing"
```
