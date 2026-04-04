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
