package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverMediaFiles(t *testing.T) {
	// Create a temp directory simulating an SD card
	root := t.TempDir()

	// Create directory structure
	dcim := filepath.Join(root, "DCIM", "100GOPRO")
	os.MkdirAll(dcim, 0755)
	os.MkdirAll(filepath.Join(root, "MISC"), 0755)

	// Create files
	os.WriteFile(filepath.Join(dcim, "GX010001.MP4"), []byte("video"), 0644)
	os.WriteFile(filepath.Join(dcim, "GX010002.MP4"), []byte("video2"), 0644)
	os.WriteFile(filepath.Join(root, "PICT0001.jpg"), []byte("photo"), 0644)
	os.WriteFile(filepath.Join(root, "readme.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(root, "MISC", "log.bin"), []byte("bin"), 0644)

	// Hidden dirs should be skipped
	hidden := filepath.Join(root, ".Spotlight-V100")
	os.MkdirAll(hidden, 0755)
	os.WriteFile(filepath.Join(hidden, "store.jpg"), []byte("hidden"), 0644)

	// Resource fork files should be skipped
	os.WriteFile(filepath.Join(dcim, "._GX010001.MP4"), []byte("fork"), 0644)

	files, err := DiscoverMediaFiles(root)
	if err != nil {
		t.Fatalf("DiscoverMediaFiles: %v", err)
	}

	// Should find all 5 non-hidden, non-resource-fork files
	if len(files) != 5 {
		t.Fatalf("got %d files, want 5: %v", len(files), files)
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
		"readme.txt",
		"MISC/log.bin",
	}
	for _, w := range want {
		if !relPaths[w] {
			t.Errorf("missing file %q in results", w)
		}
	}
}
