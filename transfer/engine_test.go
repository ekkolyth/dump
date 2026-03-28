package transfer

import (
	"os"
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

	e, err := NewEngine(cards, t.TempDir(), 2, 3)
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
