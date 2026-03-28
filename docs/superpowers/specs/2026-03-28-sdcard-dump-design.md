# SD Card Dump CLI — Design Spec

## Overview

A Go CLI tool using Charm's Bubble Tea and Lip Gloss that provides an interactive TUI for importing media files from SD cards (or any mounted volume) to a local destination. The tool features a wizard-style interface, a drillable file browser, concurrent rsync-based transfers with aggressive retry logic, and a real-time progress dashboard.

## Tech Stack

- **Language:** Go
- **TUI framework:** Bubble Tea (github.com/charmbracelet/bubbletea)
- **Styling:** Lip Gloss (github.com/charmbracelet/lipgloss)
- **Transfer engine:** rsync (system binary, called as subprocess)
- **Drive discovery:** diskutil (macOS, plist output parsed in Go)
- **Platform:** macOS (darwin)

## Architecture

Hybrid Bubble Tea architecture:
- Single top-level `tea.Model` with a state machine driving the wizard steps
- Complex UI elements (multi-select list, file browser, progress dashboard) are standalone Bubble Tea components embedded in the top-level model
- Transfer engine runs in a goroutine, communicating with the dashboard component via channels/messages

### State Machine

```
SourceSelect → DestinationSelect → Confirmation → Transfer
     ↑              ↑                    ↑
     └── Esc ───────└──── Esc ───────────┘
```

Step 4 (Transfer) is not reversible once started.

## Wizard Steps

### Step 1: Source Selection

A multi-toggle list of all mounted volumes. Each entry shows:
- Volume name
- Mount point
- Disk identifier
- Total size / free space
- Filesystem type
- Internal vs external

External drives are sorted to the top of the list. At least one source must be selected to proceed.

**Controls:** Toggle with `Space`, proceed with `Enter`, `j`/`k` or arrow keys to navigate.

### Step 2: Destination Selection

#### Drive List
Same drive list as Step 1, but single-select. Selecting a drive enters the file browser for that drive.

#### File Browser Component
A standalone Bubble Tea component for selecting a destination directory:

- Shows directories only (no files)
- Current path displayed at the top as a breadcrumb
- `Space` to drill into a folder
- `Enter` to select the current directory as the destination
- `Esc` or `..` entry to go up one level
- "Create new folder" option at the bottom — triggers inline text input for the folder name
- Sorted alphabetically
- Hidden folders (dotfiles) excluded
- Emits a message with the chosen path when the user confirms

### Step 3: Confirmation

Displays a summary before starting:
- List of selected source cards with file counts and estimated total size per card
- Destination path
- Media file extensions list (for transparency)
- "Start Import" / "Go Back" actions

File counts and sizes are calculated by walking the source cards and filtering by media extensions.

### Step 4: Transfer & Dashboard

Real-time progress dashboard (standalone Bubble Tea component):

**Top section — per-card progress:**
- Card name and volume name
- Progress bar (files completed / total)
- Bytes transferred
- Current file being transferred with size and speed

**Overall stats bar:**
- Total files completed / total
- Total bytes transferred / total
- ETA

**Bottom section — scrollable log:**
- Completed transfers (with file size)
- Retry attempts (with retry count)
- Failures (after all retries exhausted)

**On completion:**
- Progress bars replaced with final summary
- Per-card stats: succeeded / failed / skipped counts
- Log remains visible and scrollable

## Transfer Engine

### Concurrency

- Configurable `MAX_CONCURRENT` (default: 2) — total number of simultaneous rsync processes across all cards
- Each rsync call transfers one file
- Files are queued per card and dispatched to a shared worker pool

### rsync Invocation

Per file:
```
rsync --partial --progress --checksum <source> <destination>
```

- `--partial`: keep partially transferred files for resume
- `--progress`: machine-parseable progress output for the dashboard
- `--checksum`: verify integrity after transfer

### Retry Logic

- On failure: retry up to 5 times with exponential backoff (1s, 2s, 4s, 8s, 16s)
- `rsync --partial` resumes from where it left off on retry
- If the source volume disappears (card ejected/disconnected): pause that card's queue immediately, show a warning in the log — do not burn retries on a missing disk
- A file is only marked "failed" after all 5 retries are exhausted

### File Discovery

- Recursively walk each source card
- Filter by media file extensions (see list below)
- Build a queue of files per card
- Destination structure: `<dest>/card-<N>-<VolumeName>/` preserving the relative path from the card root. N is based on selection order.
  - Example: `DCIM/100GOPRO/GX010001.MP4` → `<dest>/card-1-Untitled/DCIM/100GOPRO/GX010001.MP4`

### Media Extensions

```
Video: .mp4, .mov, .avi, .mts, .m2ts, .mkv, .wmv, .3gp
Photo: .jpg, .jpeg, .png, .tiff, .tif, .heic, .heif
Raw:   .cr2, .cr3, .arw, .nef, .dng, .raw, .orf, .rw2, .raf, .srw
Pro:   .braw, .r3d, .prores, .mxf
Audio: .wav, .mp3, .aac, .m4a
```

Defined as a configurable list at the top of the relevant source file.

## Drive Discovery

Uses `diskutil list -plist` and `diskutil info -plist <identifier>` to get structured volume data. Parsed as plist XML in Go.

For each mounted volume, extract:
- Volume name
- Mount point
- Disk identifier (e.g., disk4s1)
- Total size / available space
- Filesystem type (FAT32, exFAT, NTFS, APFS, etc.)
- Internal vs external
- Removable / ejectable

All volumes are shown. External drives are sorted to the top. No heuristic filtering — the user decides what to import from and where to import to.

## Project Structure

```
sdcard-dump/
├── main.go                  # Entry point, initializes Bubble Tea
├── go.mod
├── go.sum
├── model.go                 # Top-level tea.Model, state machine
├── drives.go                # Drive discovery via diskutil
├── components/
│   ├── drivelist.go         # Multi/single-select drive list component
│   ├── filebrowser.go       # Directory browser component
│   └── dashboard.go         # Transfer progress dashboard component
├── transfer/
│   ├── engine.go            # Transfer orchestration, worker pool
│   ├── rsync.go             # rsync subprocess wrapper, output parsing
│   └── discovery.go         # Media file discovery and filtering
└── docs/
    └── superpowers/
        └── specs/
            └── 2026-03-28-sdcard-dump-design.md
```

## Configuration

Defined as constants/variables at the top of their respective files:
- `MAX_CONCURRENT` — max simultaneous rsync processes (default: 2)
- `MAX_RETRIES` — retry attempts per file (default: 5)
- `MEDIA_EXTENSIONS` — list of file extensions to copy

## Out of Scope

- Card ejection after transfer
- Non-macOS support (Linux/Windows)
- Duplicate detection across cards
- Thumbnail preview or file inspection
- Network destination support (this is local-to-local via rsync)
