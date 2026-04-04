// transfer/session_test.go
package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSessionID(t *testing.T) {
	id1 := GenerateSessionID()
	id2 := GenerateSessionID()
	if len(id1) != 8 {
		t.Errorf("session ID length = %d, want 8", len(id1))
	}
	if id1 == id2 {
		t.Error("two generated session IDs should not be equal")
	}
}

func TestSourceMetadataMarshal(t *testing.T) {
	m := DumpMetadata{
		SessionID: "abc12345",
		Role:      "source",
		CardIndex: 0,
		CardName:  "card-1-CANON",
		StartedAt: "2026-04-04T14:30:00Z",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DumpMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SessionID != "abc12345" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "abc12345")
	}
	if got.Role != "source" {
		t.Errorf("Role = %q, want %q", got.Role, "source")
	}
	if got.CardIndex != 0 {
		t.Errorf("CardIndex = %d, want 0", got.CardIndex)
	}
}

func TestDestMetadataMarshal(t *testing.T) {
	m := DumpMetadata{
		SessionID:     "abc12345",
		Role:          "destination",
		SourceCardIDs: []int{0, 1},
		StartedAt:     "2026-04-04T14:30:00Z",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DumpMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Role != "destination" {
		t.Errorf("Role = %q, want %q", got.Role, "destination")
	}
	if len(got.SourceCardIDs) != 2 {
		t.Fatalf("SourceCardIDs len = %d, want 2", len(got.SourceCardIDs))
	}
}

func TestProgressMetadataMarshal(t *testing.T) {
	p := ProgressMetadata{
		SessionID: "abc12345",
		Completed: map[int][]string{
			0: {"DCIM/100CANON/IMG_0001.CR3"},
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProgressMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Completed[0]) != 1 {
		t.Fatalf("Completed[0] len = %d, want 1", len(got.Completed[0]))
	}
	if got.Completed[0][0] != "DCIM/100CANON/IMG_0001.CR3" {
		t.Errorf("Completed[0][0] = %q, want %q", got.Completed[0][0], "DCIM/100CANON/IMG_0001.CR3")
	}
}

func TestProgressTracker_MarkAndCheck(t *testing.T) {
	dir := t.TempDir()
	pt := NewProgressTracker(dir, "sess1")

	if pt.IsComplete(0, "a.mp4") {
		t.Error("should not be complete before marking")
	}

	pt.MarkComplete(0, "a.mp4")

	if !pt.IsComplete(0, "a.mp4") {
		t.Error("should be complete after marking")
	}
	if pt.IsComplete(0, "b.mp4") {
		t.Error("b.mp4 should not be complete")
	}
	if pt.IsComplete(1, "a.mp4") {
		t.Error("card 1 a.mp4 should not be complete")
	}
}

func TestProgressTracker_PersistAndReload(t *testing.T) {
	dir := t.TempDir()
	pt := NewProgressTracker(dir, "sess1")
	pt.MarkComplete(0, "a.mp4")
	pt.MarkComplete(0, "b.mp4")
	pt.MarkComplete(1, "c.mov")

	// Create a new tracker pointing at the same dir — should load state
	pt2 := NewProgressTracker(dir, "sess1")
	if !pt2.IsComplete(0, "a.mp4") {
		t.Error("reloaded tracker should have a.mp4 complete")
	}
	if !pt2.IsComplete(1, "c.mov") {
		t.Error("reloaded tracker should have c.mov complete")
	}
}

func TestProgressTracker_IgnoresWrongSession(t *testing.T) {
	dir := t.TempDir()
	pt := NewProgressTracker(dir, "sess1")
	pt.MarkComplete(0, "a.mp4")

	// New tracker with different session ID should NOT load old data
	pt2 := NewProgressTracker(dir, "sess2")
	if pt2.IsComplete(0, "a.mp4") {
		t.Error("tracker with different session should not load old progress")
	}
}

func TestResumeRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Simulate: engine wrote metadata, transferred some files, then stopped
	sessionID := "testresume"
	WriteDumpMetadata(srcDir, DumpMetadata{
		SessionID: sessionID,
		Role:      "source",
		CardIndex: 0,
		CardName:  "card-1-TEST",
		StartedAt: "2026-04-04T00:00:00Z",
	})
	WriteDumpMetadata(destDir, DumpMetadata{
		SessionID:     sessionID,
		Role:          "destination",
		SourceCardIDs: []int{0},
		StartedAt:     "2026-04-04T00:00:00Z",
	})

	// Mark one file as already completed
	pt := NewProgressTracker(destDir, sessionID)
	pt.MarkComplete(0, "already-done.mp4")

	// Verify the tracker loads correctly on a fresh instance
	pt2 := NewProgressTracker(destDir, sessionID)
	if !pt2.IsComplete(0, "already-done.mp4") {
		t.Error("expected already-done.mp4 to be complete after reload")
	}
	if pt2.IsComplete(0, "not-done.mp4") {
		t.Error("expected not-done.mp4 to NOT be complete")
	}

	// Verify volume scanning finds the drives
	scanRoot := t.TempDir()
	vol1 := filepath.Join(scanRoot, "SRC")
	vol2 := filepath.Join(scanRoot, "DST")
	os.MkdirAll(vol1, 0755)
	os.MkdirAll(vol2, 0755)

	WriteDumpMetadata(vol1, DumpMetadata{SessionID: sessionID, Role: "source", CardIndex: 0})
	WriteDumpMetadata(vol2, DumpMetadata{SessionID: sessionID, Role: "destination"})

	matches := FindAllSessionVolumes(scanRoot, sessionID)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	srcMount, found := FindVolumeBySession(scanRoot, sessionID, "source", 0)
	if !found {
		t.Fatal("expected to find source volume")
	}
	if srcMount != vol1 {
		t.Errorf("source mount = %q, want %q", srcMount, vol1)
	}

	destMount, found := FindVolumeBySession(scanRoot, sessionID, "destination", -1)
	if !found {
		t.Fatal("expected to find destination volume")
	}
	if destMount != vol2 {
		t.Errorf("dest mount = %q, want %q", destMount, vol2)
	}
}

func TestWriteAndReadDumpMetadata(t *testing.T) {
	dir := t.TempDir()
	meta := DumpMetadata{
		SessionID: "test123",
		Role:      "source",
		CardIndex: 0,
		CardName:  "card-1-TEST",
		StartedAt: "2026-04-04T00:00:00Z",
	}
	if err := WriteDumpMetadata(dir, meta); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadDumpMetadata(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SessionID != "test123" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "test123")
	}
	if got.CardName != "card-1-TEST" {
		t.Errorf("CardName = %q, want %q", got.CardName, "card-1-TEST")
	}
}
