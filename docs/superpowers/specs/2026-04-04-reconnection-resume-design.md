# Reconnection & Resume Design

## Problem

Dump is used in vehicles on the road. External drives (both source SD cards and destination drives) can disconnect from vibration or accidental bumps. The current engine retries 5 times over ~31 seconds then permanently marks files as failed. Users need to be able to plug a drive back in and have the transfer pick up where it left off, indefinitely.

## Approach

Poll-and-resume in the engine. Each transfer goroutine independently waits for its volumes. rsync `--partial` handles partial file resume. A `dump.json` metadata file on each drive is the source of truth for drive identity and session state.

## Design

### 1. Drive Identity via `dump.json`

When a transfer session starts, the engine writes a `dump.json` to the root of every involved drive.

**Source card example (`/Volumes/CANON_EOS/dump.json`):**
```json
{
  "session_id": "a1b2c3d4",
  "role": "source",
  "card_index": 0,
  "card_name": "card-1-CANON_EOS",
  "started_at": "2026-04-04T14:30:00Z"
}
```

**Destination drive example (`/Volumes/SSD_BACKUP/dump.json`):**
```json
{
  "session_id": "a1b2c3d4",
  "role": "destination",
  "source_card_ids": [0, 1],
  "started_at": "2026-04-04T14:30:00Z"
}
```

Drives are never identified by mount path or volume name — only by their `dump.json` content. This handles "UNTITLED" drives and drives that remount at different paths.

### 2. Progress Tracking via `dump-progress.json`

A `dump-progress.json` file on the **destination** drive tracks completed files:

```json
{
  "session_id": "a1b2c3d4",
  "completed": {
    "0": ["DCIM/100CANON/IMG_0001.CR3", "DCIM/100CANON/IMG_0002.CR3"],
    "1": ["DCIM/100MSDCF/DSC00001.ARW"]
  }
}
```

Updated after each file completes. This is the source of truth for what has been transferred — resume reads this file to determine remaining work.

If the destination drive disconnects, progress writes will fail silently until the drive reconnects. The engine tracks completed files in memory during the session, so no progress is lost — the on-disk file is synced when the destination comes back. On resume via CLI, the file is read fresh from the destination.

### 3. Reconnection Wait Loop

The current retry loop in `processJob` is replaced with a two-layer approach:

**Outer layer — volume wait:** Before any rsync attempt, check both source and destination volumes. If either is missing:
- Emit `EventCardWaiting` (new event type)
- Poll every 2 seconds, scanning mounted volumes for a `dump.json` matching the session ID and expected card index (or role)
- No timeout — wait indefinitely
- When the volume reappears, update the engine's mount path reference, emit `EventCardResumed`, and continue

**Inner layer — rsync retry:** Once both volumes are confirmed present, run rsync. If rsync fails for a non-disconnection reason (transient I/O glitch), retry up to 3 times with short backoff (1s, 2s, 4s). If during retry we detect a volume went missing again, drop back to the outer wait loop.

The engine accepts a `context.Context` so the UI can cancel all wait loops when the user presses Ctrl+C.

### 4. Dashboard UI During Wait

When a card enters the waiting state, the dashboard shows:

```
⏳ Waiting for CANON_EOS to reconnect...
```

When it reconnects:

```
↻ CANON_EOS reconnected, resuming...
```

Then flips back to normal transfer progress. Other cards that are still connected continue transferring independently.

### 5. Ctrl+C Behavior

- **First Ctrl+C:** Cancel the engine context. All wait loops and in-flight transfers stop. Return to the wizard home screen. The `dump.json` and `dump-progress.json` files remain on drives.
- **Second Ctrl+C (within 3 seconds):** Exit the app. Print the resume command to stdout:
  ```
  To resume this session: dump --resume a1b2c3d4
  ```

### 6. Resume — Three Ways

#### A. CLI flag: `dump --resume <session_id>`

1. Scan all mounted volumes for `dump.json` files matching the session ID
2. If any expected drives are missing, use the same wait-and-poll loop (with dashboard UI showing which drives are missing)
3. Read `dump-progress.json` from the destination to get completed files
4. Discover files on each source, subtract the completed set
5. Build the job queue with only remaining files
6. Skip the wizard, go straight to the transfer dashboard

#### B. Home screen "Resume Session"

The home screen gets a new option:

```
[1] New Session
[2] Resume Session
```

"Resume Session" flow:
1. Show a drive picker — user selects the drives they've plugged in
2. Read `dump.json` from each selected drive
3. Group by `session_id`, validate the set (all sources + destination accounted for)
4. If any drives from the session are missing, show which ones and let the user either plug them in and re-select, or skip that card entirely
5. Jump to the transfer dashboard with remaining files

#### C. Ctrl+C once during transfer

Returns to home screen. User can select "Resume Session" to pick back up, or start a new session.

### 7. Session Cleanup

- **Successful completion:** Delete `dump.json` from all source cards and `dump.json` + `dump-progress.json` from the destination. No metadata left behind.
- **Quit without finishing:** Files stay on drives. They're small and harmless, and they're what makes resume work.
- **Stale sessions:** If the user starts a new session to drives that already have a `dump.json` from an old session, silently overwrite. Starting a fresh dump signals intent to replace the old session.

### 8. Volume Scanning

New utility function to scan for drives by `dump.json` content:

```go
func FindVolumeBySession(sessionID string, role string, cardIndex int) (mountPoint string, found bool)
```

Scans `/Volumes/*` (macOS). Reads `dump.json` from each mount root. Returns the mount point of the matching drive. Used by both the reconnection wait loop and the `--resume` flag.

### 9. Engine Changes Summary

- `Engine` gains a `context.Context` field, passed through to all goroutines
- `processJob` replaces the fixed retry loop with volume-wait + rsync-retry
- New `waitForVolume` method: polls every 2s, scans for `dump.json`, respects context cancellation
- New event types: `EventCardWaiting`, `EventCardResumed`
- `NewEngine` writes `dump.json` to all drives at session start
- `dump-progress.json` updated after each `EventFileComplete`
- Mount paths in `CardSource` are mutable — updated when a drive remounts at a different path

### 10. CLI Changes Summary

- New `--resume` flag on the root command
- Resume path: decode session ID → scan drives → wait for missing → build queue → transfer
- Home screen: add "Resume Session" option before the source selection step
