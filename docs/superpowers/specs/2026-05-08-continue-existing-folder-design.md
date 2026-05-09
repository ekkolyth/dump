# Continue Existing Folder — Design

Date: 2026-05-08
Status: Approved (pre-implementation)

## Problem

A photographer fills more cards than they have card-reader slots, so they cannot dump every card in one `dump` session. Today, a second `dump` session creates a *new* event folder (`YY.MM.DD - CLIENT - EVENT`) — there is no way to add the remaining cards to the folder produced by the first session.

## Goal

Add a wizard path that lets the user select an existing event folder on a destination drive and continue dumping into it. Newly dumped cards are numbered starting one above the highest existing `CARD N` folder, so card numbers remain contiguous across sessions.

## Non-goals

- No history/recent-dumps log. The user picks the folder explicitly each session.
- No stable disk identifier (UUID) tracking. Folder selection is by direct navigation.
- No merging into an *incomplete* prior session. This feature is for adding *new* cards to a completed event folder, not resuming. (`Resume Session` already covers interrupted transfers.)
- No automatic restoration of prior `dump.json` / `dump-progress.json` files.

## User flow

```
stepSourceSelect
   └─> stepDestSelect ──┬──[drive picked, continueMode=false]──> stepClientInput → stepEventInput → stepConfirm → stepTransfer
                        │
                        ├──["Continue Existing Folder" extra item]──> sets continueMode=true, stays on stepDestSelect (re-rendered)
                        │
                        └──[drive picked, continueMode=true]──> stepContinueBrowse → stepConfirm → stepTransfer
                                                                  (pick event folder)
```

The continue branch reuses `stepDestSelect`: the extra item flips `continueMode` to `true` without changing the step, so the user picks a destination drive on the same screen. Only one new step is added: `stepContinueBrowse`.

## Wizard step changes

### `stepDestSelect`

- New `ExtraItems` entry on the dest drive list: `"Continue Existing Folder"`.
- When the user selects this extra item, set `m.continueMode = true` and remain on `stepDestSelect`. The drive list re-renders with title indicating the user is now picking the destination drive *for the continue flow*. No flow change beyond the title — drive selection still triggers `DriveSelectedMsg`.
- On `DriveSelectedMsg`:
  - If `m.continueMode` is `false`: existing path (set `destPath`, advance to `stepClientInput`).
  - If `m.continueMode` is `true`: set `destPath`, instantiate `FileBrowser` rooted at `destPath`, advance to `stepContinueBrowse`.

### `stepContinueBrowse` (new)

- Hosts `components.FileBrowserModel` already present at `internal/components/filebrowser.go` (currently unused).
- Browser starts at the dest drive mount point.
- User navigates and presses `enter` on an existing top-level event folder (e.g. `26.04.04 - CLIENT - EVENT`). `esc` returns to dest select.
- On `FolderSelectedMsg`:
  1. `m.continueFolder = filepath.Base(msg.Path)`
  2. `m.destPath = filepath.Dir(msg.Path)` (already equals dest mount point in normal usage; this preserves it if user ever drilled deeper)
  3. Scan `msg.Path` for `CARD N` subdirectories. `m.continueStartCard = max(N)` across matches; `0` if none.
  4. Build `cardSummaries` (file count + bytes per source card) — same logic currently used at `tui/model.go:558-572`.
  5. `m.step = stepConfirm`.

### `stepConfirm`

- Branches on `m.continueMode`:
  - **New dump (existing)**: title `Step 5 — Confirm Import`, header line `Event  YY.MM.DD - CLIENT - EVENT`, cards named `<eventFolder> - CARD 1`, `… - CARD 2`, …
  - **Continue mode**: title `Continuing — Add Cards to Existing Folder`, header lines `Folder  <continueFolder>` and `Destination  <destPath>`, cards named `CARD <continueStartCard+1>`, `CARD <continueStartCard+2>`, … with the same per-card file count and total bytes display.

### `startTransfer` continue branch

```go
eventFolder := m.continueFolder
startIdx := m.continueStartCard

cards := make([]transfer.CardSource, len(m.selectedSources))
for i, src := range m.selectedSources {
    cards[i] = transfer.CardSource{
        MountPoint: src.MountPoint,
        VolumeName: src.VolumeName,
        CardIndex:  i,
        FolderName: fmt.Sprintf("CARD %d", startIdx+i+1),
    }
}

engine, err := transfer.NewEngine(ctx, cards, m.destPath, eventFolder, ...)
```

The existing `startTransfer` path applies in normal mode unchanged.

## Card index scan

Helper added to the `transfer` package (or kept inline in `tui/model.go` if small enough):

```go
// MaxCardIndex scans dir for entries matching ^CARD (\d+)$ and returns the
// highest N found. Returns 0 if none.
func MaxCardIndex(dir string) int
```

- Uses `os.ReadDir`.
- Match regex: `^CARD (\d+)$` (case-sensitive — matches the format engine writes).
- Skips non-directories.
- Returns 0 on read error or empty match set (caller starts numbering at 1).

## State additions to `model`

```go
type model struct {
    ...
    continueMode      bool   // true when user is in the Continue Existing Folder path
    continueFolder    string // basename of selected event folder
    continueStartCard int    // max CARD N found in selected folder
    fileBrowser       components.FileBrowserModel
    ...
}
```

`handleBack` gains:
- `case stepContinueBrowse:` → return to `stepDestSelect`.
- On `stepDestSelect` while `continueMode` is true: `esc` clears `continueMode` and returns to `stepSourceSelect` (so user can exit the continue branch entirely).

`resetToMainMenu` clears `continueMode`, `continueFolder`, `continueStartCard`, and `fileBrowser`.

## Engine, sessions, metadata

No changes to the transfer engine, `dump.json`, or `dump-progress.json` formats.

- A continue-mode dump generates a **new session ID** via the standard `NewEngine` path.
- `dump.json` written to the destination drive root by `NewEngine` will overwrite the prior session's `dump.json` (same file path). This is acceptable: the prior session is complete, its metadata is no longer load-bearing.
- `dump-progress.json` likewise. If a prior `dump-progress.json` exists for a different session ID, `NewProgressTracker` already discards it (mismatched session ID — see `transfer/session.go:86`), so the new session starts with an empty progress map. No code change needed.
- After a continue-mode session completes, the existing cleanup at `tui/model.go:660-664` removes `dump.json` and `dump-progress.json` from the destination, identical to the new-dump path.

## UI / View changes

- `tui/model.go` `View()` adds a `case stepContinueBrowse:` rendering the file browser with a header title (e.g. `Continue — Select Existing Event Folder`) and the browser's own help line.
- `stepConfirm` view branches on `continueMode` to render the alternate header and box content described above.
- `stepDestSelect` view: when `continueMode` is true, change the title from `Step 2 — Select Destination Drive` to `Continue — Select Destination Drive`. Otherwise unchanged.

## Edge cases

- **Empty folder picked** (no `CARD N` subdirs): `continueStartCard = 0`, new cards numbered from `CARD 1`. Functionally equivalent to dumping into a pre-created empty folder.
- **Non-event folder picked** (e.g. user navigates into `Documents`): same as empty folder — starts at `CARD 1`. No special protection. The user is choosing the folder; trust the user.
- **`CARD <very large N>` already present**: Go's `int` handles it; folder name is just a string.
- **Dest drive disappears mid-transfer**: existing reconnection logic in the engine applies unchanged.
- **Continue-mode `esc` from `stepContinueBrowse`**: returns to `stepDestSelect` with `continueMode` still true, letting user pick a different drive.
- **Continue-mode `esc` from `stepDestSelect`** (continue path): clears `continueMode`, returns to `stepSourceSelect`.

## Files touched

- `internal/tui/model.go` — wizard state, new step, view branches, start-transfer branch.
- `internal/transfer/` — add `MaxCardIndex` helper (file: `cards.go` or extend `discovery.go`).
- `internal/components/filebrowser.go` — no change; consumed as-is.
- Tests: `internal/transfer/cards_test.go` covering `MaxCardIndex` against synthetic dirs (empty, single, multiple, gaps, non-matching names, `CARD 1` vs `CARD 10` ordering).

## Out of scope (future work, if requested)

- Recent-dumps log / quick-pick of recent destinations.
- Stable disk identification across remounts.
- Detecting and warning when picked folder belongs to a still-incomplete session (`dump-progress.json` present).
