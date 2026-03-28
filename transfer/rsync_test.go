package transfer

import "testing"

func TestParseProgressLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantOK  bool
		wantPct int
		wantSpd string
		wantFin bool
	}{
		{
			name:    "incremental progress",
			line:    "     262144  51%   63.97KB/s   00:00:03",
			wantOK:  true,
			wantPct: 51,
			wantSpd: "63.97KB/s",
			wantFin: false,
		},
		{
			name:    "final line with xfer info",
			line:    "     512000 100%   49.97KB/s   00:00:10 (xfer#1, to-check=0/1)",
			wantOK:  true,
			wantPct: 100,
			wantSpd: "49.97KB/s",
			wantFin: true,
		},
		{
			name:    "large file progress",
			line:    "  104857600  25%    2.10MB/s   00:01:30",
			wantOK:  true,
			wantPct: 25,
			wantSpd: "2.10MB/s",
			wantFin: false,
		},
		{
			name:   "filename line",
			line:   "GX010047.MP4",
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := ParseProgressLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if p.Percentage != tt.wantPct {
				t.Errorf("Percentage = %d, want %d", p.Percentage, tt.wantPct)
			}
			if p.Speed != tt.wantSpd {
				t.Errorf("Speed = %q, want %q", p.Speed, tt.wantSpd)
			}
			if p.IsFinal != tt.wantFin {
				t.Errorf("IsFinal = %v, want %v", p.IsFinal, tt.wantFin)
			}
		})
	}
}

func TestScanCRLF(t *testing.T) {
	data := []byte("line1\rline2\nline3\r")

	var tokens []string
	offset := 0
	for offset < len(data) {
		advance, token, _ := ScanCRLF(data[offset:], false)
		if advance == 0 {
			break
		}
		if token != nil {
			tokens = append(tokens, string(token))
		}
		offset += advance
	}

	if len(tokens) != 3 {
		t.Fatalf("got %d tokens, want 3: %v", len(tokens), tokens)
	}
	expected := []string{"line1", "line2", "line3"}
	for i, want := range expected {
		if tokens[i] != want {
			t.Errorf("token[%d] = %q, want %q", i, tokens[i], want)
		}
	}
}
