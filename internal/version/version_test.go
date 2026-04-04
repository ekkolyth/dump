package version

import "testing"

func TestVersionDefault(t *testing.T) {
	if Version == "" {
		t.Fatal("Version should not be empty")
	}
	if Version != "dev" {
		t.Fatalf("Default version should be 'dev', got %q", Version)
	}
}
