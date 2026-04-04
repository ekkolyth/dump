package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVolumeMissing(t *testing.T) {
	if VolumeMissing("/") {
		t.Error("VolumeMissing('/') = true, want false")
	}
	if !VolumeMissing("/Volumes/NoSuchVolume_XYZ_12345") {
		t.Error("VolumeMissing for non-existent path = false, want true")
	}
}

func TestTransferJobQueue(t *testing.T) {
	q := NewJobQueue()

	j1 := &TransferJob{File: MediaFile{RelPath: "a.mp4"}, CardIndex: 0}
	j2 := &TransferJob{File: MediaFile{RelPath: "b.mp4"}, CardIndex: 0}
	j3 := &TransferJob{File: MediaFile{RelPath: "c.mp4"}, CardIndex: 1}

	q.Push(j1)
	q.Push(j2)
	q.Push(j3)

	if q.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", q.Len())
	}

	got := q.Pop()
	if got.File.RelPath != "a.mp4" {
		t.Errorf("Pop() = %q, want %q", got.File.RelPath, "a.mp4")
	}

	if q.Len() != 2 {
		t.Fatalf("Len() after Pop = %d, want 2", q.Len())
	}
}

func TestEngineCreation(t *testing.T) {
	tmpDir := t.TempDir()

	cards := []CardSource{
		{MountPoint: tmpDir, VolumeName: "TestCard", CardIndex: 0},
	}

	// Create a media file
	os.WriteFile(tmpDir+"/test.mp4", []byte("video"), 0644)

	e, err := NewEngine(context.Background(), cards, t.TempDir(), "", 2, 3)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if e.MaxConcurrent != 2 {
		t.Errorf("MaxConcurrent = %d, want 2", e.MaxConcurrent)
	}
	if e.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", e.MaxRetries)
	}
	if len(e.Cards) != 1 {
		t.Fatalf("Cards = %d, want 1", len(e.Cards))
	}
	if e.Cards[0].TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1", e.Cards[0].TotalFiles)
	}
}

func TestEngine_WritesMetadata(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "test.mp4"), []byte("video"), 0644)

	cards := []CardSource{
		{MountPoint: srcDir, VolumeName: "TestCard", CardIndex: 0},
	}

	e, err := NewEngine(context.Background(), cards, destDir, "", 2, 3)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if e.SessionID == "" {
		t.Error("SessionID should not be empty")
	}

	srcMeta, err := ReadDumpMetadata(srcDir)
	if err != nil {
		t.Fatalf("read source metadata: %v", err)
	}
	if srcMeta.Role != "source" {
		t.Errorf("source role = %q, want %q", srcMeta.Role, "source")
	}
	if srcMeta.SessionID != e.SessionID {
		t.Errorf("source session = %q, want %q", srcMeta.SessionID, e.SessionID)
	}

	destMeta, err := ReadDumpMetadata(destDir)
	if err != nil {
		t.Fatalf("read dest metadata: %v", err)
	}
	if destMeta.Role != "destination" {
		t.Errorf("dest role = %q, want %q", destMeta.Role, "destination")
	}
}

func TestEngine_WaitForVolume_AlreadyPresent(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "test.mp4"), []byte("video"), 0644)

	ctx := context.Background()
	cards := []CardSource{
		{MountPoint: srcDir, VolumeName: "TestCard", CardIndex: 0},
	}

	e, err := NewEngine(ctx, cards, destDir, "", 2, 3)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Source dump.json already written by NewEngine, volume is present
	scanRoot := filepath.Dir(srcDir)
	mount, err := e.waitForVolume(scanRoot, "source", 0)
	if err != nil {
		t.Fatalf("waitForVolume: %v", err)
	}
	if mount != srcDir {
		t.Errorf("mount = %q, want %q", mount, srcDir)
	}
}

func TestEngine_WaitForVolume_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	e := &Engine{
		SessionID: "nosuchsession",
		ctx:       ctx,
	}

	_, err := e.waitForVolume(t.TempDir(), "source", 0)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
