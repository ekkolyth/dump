package upgrade

import (
	"runtime"
	"testing"
)

func TestAssetName(t *testing.T) {
	name := assetName("1.2.3", "darwin", "arm64")
	if name != "dump_1.2.3_darwin_arm64.tar.gz" {
		t.Fatalf("unexpected asset name: %s", name)
	}
}


func TestAssetNameCurrentPlatform(t *testing.T) {
	name := assetName("1.0.0", runtime.GOOS, runtime.GOARCH)
	if name == "" {
		t.Fatal("asset name should not be empty")
	}
}

func TestParseVersionTag(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v0.1.0", "0.1.0"},
	}
	for _, tt := range tests {
		got := parseVersionTag(tt.tag)
		if got != tt.want {
			t.Errorf("parseVersionTag(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}
