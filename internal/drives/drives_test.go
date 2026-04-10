package drives

import (
	"os"
	"path/filepath"
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

func TestDiscoverNetworkVolumes(t *testing.T) {
	root := t.TempDir()

	// Create fake mount points
	os.MkdirAll(filepath.Join(root, "NAS-Share"), 0755)
	os.MkdirAll(filepath.Join(root, "Backup-Drive"), 0755)
	os.MkdirAll(filepath.Join(root, ".hidden"), 0755)
	os.MkdirAll(filepath.Join(root, "Macintosh HD"), 0755)
	os.MkdirAll(filepath.Join(root, "Already-Found"), 0755)

	// "Already-Found" is in the seen set (already discovered by diskutil)
	seen := map[string]bool{
		filepath.Join(root, "Already-Found"): true,
	}

	drives := discoverNetworkVolumes(root, seen)

	// Should find NAS-Share and Backup-Drive only
	names := make(map[string]bool)
	for _, d := range drives {
		names[d.VolumeName] = true
		if !d.IsNetwork {
			t.Errorf("drive %q: IsNetwork = false, want true", d.VolumeName)
		}
		if d.FilesystemName != "Network" {
			t.Errorf("drive %q: FilesystemName = %q, want %q", d.VolumeName, d.FilesystemName, "Network")
		}
		if d.TotalSize <= 0 {
			t.Errorf("drive %q: TotalSize = %d, want > 0", d.VolumeName, d.TotalSize)
		}
	}

	if !names["NAS-Share"] {
		t.Error("missing NAS-Share in results")
	}
	if !names["Backup-Drive"] {
		t.Error("missing Backup-Drive in results")
	}
	if names[".hidden"] {
		t.Error("hidden directory should be skipped")
	}
	if names["Macintosh HD"] {
		t.Error("Macintosh HD should be skipped")
	}
	if names["Already-Found"] {
		t.Error("already-seen drive should be skipped")
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
