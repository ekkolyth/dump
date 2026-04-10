package drives

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

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
	IsNetwork                      bool
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
	AllDisks   []string `plist:"AllDisks"`
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

		// Skip macOS system volumes (Preboot, Recovery, VM, Update, etc.)
		if strings.HasPrefix(info.MountPoint, "/System/Volumes/") {
			continue
		}

		// Skip disk images (e.g. mounted .dmg installers)
		if info.BusProtocol == "Disk Image" {
			continue
		}

		// Fall back to mount point basename if volume name is empty
		if info.VolumeName == "" && info.MountPoint != "" {
			info.VolumeName = filepath.Base(info.MountPoint)
		}

		drives = append(drives, info)
	}

	// Scan /Volumes for network mounts that diskutil doesn't report
	seen := make(map[string]bool, len(drives))
	for _, d := range drives {
		seen[d.MountPoint] = true
	}
	drives = append(drives, discoverNetworkVolumes("/Volumes", seen)...)

	// Sort: external first, then by volume name
	sort.Slice(drives, func(i, j int) bool {
		if drives[i].IsExternal() != drives[j].IsExternal() {
			return drives[i].IsExternal()
		}
		return drives[i].VolumeName < drives[j].VolumeName
	})

	return drives, nil
}

// discoverNetworkVolumes scans a directory for mounted volumes not already
// in the seen set. This catches SMB/AFP/NFS mounts that diskutil misses.
func discoverNetworkVolumes(root string, seen map[string]bool) []DiskInfo {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var drives []DiskInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()

		// Skip hidden entries and the boot volume
		if strings.HasPrefix(name, ".") || name == "Macintosh HD" {
			continue
		}

		mountPoint := filepath.Join(root, name)
		if seen[mountPoint] {
			continue
		}

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			continue
		}

		totalSize := int64(stat.Bsize) * int64(stat.Blocks)
		freeSpace := int64(stat.Bsize) * int64(stat.Bavail)

		drives = append(drives, DiskInfo{
			VolumeName:                     name,
			MountPoint:                     mountPoint,
			TotalSize:                      totalSize,
			FreeSpace:                      freeSpace,
			FilesystemName:                 "Network",
			RemovableMediaOrExternalDevice: true,
			IsNetwork:                      true,
		})
	}
	return drives
}
