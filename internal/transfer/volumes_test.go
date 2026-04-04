package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindVolumeBySession_Source(t *testing.T) {
	root := t.TempDir()
	vol1 := filepath.Join(root, "CARD_A")
	vol2 := filepath.Join(root, "CARD_B")
	os.MkdirAll(vol1, 0755)
	os.MkdirAll(vol2, 0755)

	WriteDumpMetadata(vol1, DumpMetadata{
		SessionID: "sess1",
		Role:      "source",
		CardIndex: 0,
	})
	WriteDumpMetadata(vol2, DumpMetadata{
		SessionID: "sess1",
		Role:      "source",
		CardIndex: 1,
	})

	mount, found := FindVolumeBySession(root, "sess1", "source", 0)
	if !found {
		t.Fatal("expected to find volume for card 0")
	}
	if mount != vol1 {
		t.Errorf("mount = %q, want %q", mount, vol1)
	}

	mount, found = FindVolumeBySession(root, "sess1", "source", 1)
	if !found {
		t.Fatal("expected to find volume for card 1")
	}
	if mount != vol2 {
		t.Errorf("mount = %q, want %q", mount, vol2)
	}
}

func TestFindVolumeBySession_Destination(t *testing.T) {
	root := t.TempDir()
	vol := filepath.Join(root, "SSD")
	os.MkdirAll(vol, 0755)

	WriteDumpMetadata(vol, DumpMetadata{
		SessionID: "sess1",
		Role:      "destination",
	})

	mount, found := FindVolumeBySession(root, "sess1", "destination", -1)
	if !found {
		t.Fatal("expected to find destination volume")
	}
	if mount != vol {
		t.Errorf("mount = %q, want %q", mount, vol)
	}
}

func TestFindVolumeBySession_NotFound(t *testing.T) {
	root := t.TempDir()
	vol := filepath.Join(root, "CARD")
	os.MkdirAll(vol, 0755)

	WriteDumpMetadata(vol, DumpMetadata{
		SessionID: "other",
		Role:      "source",
		CardIndex: 0,
	})

	_, found := FindVolumeBySession(root, "sess1", "source", 0)
	if found {
		t.Error("should not find volume for wrong session")
	}
}

func TestFindAllSessionVolumes(t *testing.T) {
	root := t.TempDir()
	vol1 := filepath.Join(root, "CARD_A")
	vol2 := filepath.Join(root, "SSD")
	vol3 := filepath.Join(root, "UNRELATED")
	os.MkdirAll(vol1, 0755)
	os.MkdirAll(vol2, 0755)
	os.MkdirAll(vol3, 0755)

	WriteDumpMetadata(vol1, DumpMetadata{SessionID: "sess1", Role: "source", CardIndex: 0})
	WriteDumpMetadata(vol2, DumpMetadata{SessionID: "sess1", Role: "destination"})
	WriteDumpMetadata(vol3, DumpMetadata{SessionID: "other", Role: "source", CardIndex: 0})

	results := FindAllSessionVolumes(root, "sess1")
	if len(results) != 2 {
		t.Fatalf("found %d volumes, want 2", len(results))
	}
}
