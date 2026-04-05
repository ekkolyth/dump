package transfer

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Progress represents a single rsync progress update.
type Progress struct {
	Filename         string
	BytesTransferred int64
	Percentage       int
	Speed            string
	ETA              string
	IsFinal          bool
}

var progressRe = regexp.MustCompile(
	`^\s*(\d+)\s+(\d+)%\s+(\S+/s)\s+(\d+:\d+:\d+)(?:\s+\(xfer#\d+,\s+to-check=\d+/\d+\))?`,
)

var xferRe = regexp.MustCompile(`\(xfer#\d+`)

// ParseProgressLine parses a single rsync --progress output line.
func ParseProgressLine(line string) (Progress, bool) {
	trimmed := strings.TrimSpace(line)
	m := progressRe.FindStringSubmatch(trimmed)
	if m == nil {
		return Progress{}, false
	}

	bytes, _ := strconv.ParseInt(m[1], 10, 64)
	pct, _ := strconv.Atoi(m[2])

	p := Progress{
		BytesTransferred: bytes,
		Percentage:       pct,
		Speed:            m[3],
		ETA:              m[4],
		IsFinal:          xferRe.MatchString(trimmed),
	}

	return p, true
}

// ScanCRLF is a bufio.SplitFunc that splits on \r or \n.
// This is needed because rsync uses \r for incremental progress updates.
func ScanCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\r' || b == '\n' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// RsyncFile copies a single file using rsync with progress reporting.
// onProgress is called for each progress update.
// Returns nil on success, error on failure.
func RsyncFile(src, dst string, onProgress func(Progress)) error {
	args := []string{
		"--partial",
		"--progress",
		"--append",
		"--inplace",
		src,
		dst,
	}

	cmd := exec.Command("rsync", args...)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start rsync: %w", err)
	}

	currentFile := ""
	scanner := bufio.NewScanner(stdout)
	scanner.Split(ScanCRLF)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		p, ok := ParseProgressLine(line)
		if ok {
			p.Filename = currentFile
			if onProgress != nil {
				onProgress(p)
			}
		} else {
			currentFile = strings.TrimSpace(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("rsync exited %d: %s",
				exitErr.ExitCode(), strings.TrimSpace(stderrBuf.String()))
		}
		return fmt.Errorf("rsync: %w", err)
	}

	return nil
}
