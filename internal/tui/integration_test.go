package tui

import (
	"context"
	"fmt"
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

func TestIntegration_ContinueExistingFolder(t *testing.T) {
	// Two source cards, one already-populated dest folder containing CARD 1 & CARD 2.
	srcDir1 := t.TempDir()
	srcDir2 := t.TempDir()
	for _, p := range []string{
		filepath.Join(srcDir1, "DCIM", "100GOPRO"),
		filepath.Join(srcDir2, "DCIM", "100GOPRO"),
	} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	data := make([]byte, 512)
	if err := os.WriteFile(filepath.Join(srcDir1, "DCIM", "100GOPRO", "GX01.MP4"), data, 0644); err != nil {
		t.Fatalf("write src1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir2, "DCIM", "100GOPRO", "GX02.MP4"), data, 0644); err != nil {
		t.Fatalf("write src2: %v", err)
	}

	destDir := t.TempDir()
	eventFolder := "26.05.08 - CLIENT - EVENT"
	for _, n := range []string{"CARD 1", "CARD 2"} {
		if err := os.MkdirAll(filepath.Join(destDir, eventFolder, n), 0755); err != nil {
			t.Fatalf("mkdir existing %s: %v", n, err)
		}
	}

	// The engine scans VolumesRoot for source/dest dump.json files. All t.TempDir()
	// calls in a single test share the same parent, so point VolumesRoot at it.
	origVolumesRoot := transfer.VolumesRoot
	transfer.VolumesRoot = filepath.Dir(srcDir1)
	t.Cleanup(func() { transfer.VolumesRoot = origVolumesRoot })

	highest := transfer.HighestCardNumber(filepath.Join(destDir, eventFolder))
	if highest != 2 {
		t.Fatalf("HighestCardNumber = %d, want 2", highest)
	}

	cards := []transfer.CardSource{
		{MountPoint: srcDir1, VolumeName: "src1", CardIndex: 0, FolderName: fmt.Sprintf("CARD %d", highest+1)},
		{MountPoint: srcDir2, VolumeName: "src2", CardIndex: 1, FolderName: fmt.Sprintf("CARD %d", highest+2)},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine, err := transfer.NewEngine(ctx, cards, destDir, eventFolder, transfer.MaxConcurrentDefault, transfer.MaxRetriesDefault)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	done := make(chan struct{})
	go func() {
		engine.Run()
		close(done)
	}()
	for range engine.Events {
		// drain
	}
	<-done

	for _, n := range []string{"CARD 1", "CARD 2", "CARD 3", "CARD 4"} {
		if _, err := os.Stat(filepath.Join(destDir, eventFolder, n)); err != nil {
			t.Errorf("expected %s to exist: %v", n, err)
		}
	}
	for _, p := range []string{
		filepath.Join(destDir, eventFolder, "CARD 3", "DCIM", "100GOPRO", "GX01.MP4"),
		filepath.Join(destDir, eventFolder, "CARD 4", "DCIM", "100GOPRO", "GX02.MP4"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}
