package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ekkolyth/dump/internal/transfer"
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
