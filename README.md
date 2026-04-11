# dump

CLI tool for transferring files from camera cards to drives. Copies everything on the card, tracks progress, and resumes interrupted transfers.

Supports macOS and Linux. Requires `rsync`.

## Install

```bash
curl -fsSL https://kenway.me/install/dump | bash
```

## Usage

```bash
dump              # interactive TUI — select source cards and destination
dump resume <id>  # resume an interrupted session
dump upgrade      # self-update to latest release
dump version      # print version
```

## Development

Requires Go 1.24+.

```bash
# build
miso build              # or: scripts/build.sh

# test
miso test               # or: scripts/test.sh

# install dev build locally
miso local/install      # installs to $GOPATH/bin/dump

# remove dev build
miso local/uninstall
```

### Project structure

```
cmd/dump/                  # entrypoint
internal/
  tui/                     # Bubble Tea TUI (wizard flow, state machine)
  components/              # reusable TUI components (drive list, dashboard)
  drives/                  # drive discovery (diskutil + /Volumes scan)
  transfer/                # file discovery, rsync wrapper, session tracking, transfer engine
  upgrade/                 # self-update from GitHub releases
  version/                 # build-time version embedding
scripts/
  build.sh                 # production build
  test.sh                  # run tests
  install.sh               # end-user install script
  local/                   # dev convenience scripts
```
