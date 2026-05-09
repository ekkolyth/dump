package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHighestCardNumber(t *testing.T) {
	tests := []struct {
		name     string
		dirs     []string
		files    []string
		expected int
	}{
		{name: "empty dir", expected: 0},
		{name: "no matching dirs", dirs: []string{"DCIM", "Documents", "card1"}, expected: 0},
		{name: "single card", dirs: []string{"CARD 1"}, expected: 1},
		{name: "contiguous", dirs: []string{"CARD 1", "CARD 2", "CARD 3"}, expected: 3},
		{name: "gaps", dirs: []string{"CARD 1", "CARD 4", "CARD 7"}, expected: 7},
		{name: "lexical vs numeric", dirs: []string{"CARD 1", "CARD 10", "CARD 2"}, expected: 10},
		{name: "ignores files named CARD N", files: []string{"CARD 5"}, expected: 0},
		{name: "ignores lowercase", dirs: []string{"card 1", "CaRd 2"}, expected: 0},
		{name: "ignores extra suffix", dirs: []string{"CARD 1 backup", "CARD 2-old"}, expected: 0},
		{name: "ignores no-space form", dirs: []string{"CARD1", "CARD2"}, expected: 0},
		{name: "very large N", dirs: []string{"CARD 999"}, expected: 999},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range tc.dirs {
				if err := os.Mkdir(filepath.Join(dir, d), 0755); err != nil {
					t.Fatalf("mkdir %s: %v", d, err)
				}
			}
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), nil, 0644); err != nil {
					t.Fatalf("write %s: %v", f, err)
				}
			}
			got := HighestCardNumber(dir)
			if got != tc.expected {
				t.Errorf("HighestCardNumber = %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestHighestCardNumberReadError(t *testing.T) {
	got := HighestCardNumber(filepath.Join(t.TempDir(), "does-not-exist"))
	if got != 0 {
		t.Errorf("HighestCardNumber on missing dir = %d, want 0", got)
	}
}
