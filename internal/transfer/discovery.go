package transfer

import (
	"os"
	"path/filepath"
	"strings"
)

// MediaFile represents a discovered file on a source card.
type MediaFile struct {
	AbsPath string // Full path on disk
	RelPath string // Path relative to the card root
	Size    int64  // File size in bytes
}

// DiscoverMediaFiles walks a directory tree and returns all files.
// Hidden directories (starting with .) are skipped.
func DiscoverMediaFiles(root string) ([]MediaFile, error) {
	var files []MediaFile

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip files we can't read
		}

		// Skip hidden directories
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && path != root {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		// Skip macOS resource fork files (._*)
		if strings.HasPrefix(info.Name(), "._") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		files = append(files, MediaFile{
			AbsPath: path,
			RelPath: rel,
			Size:    info.Size(),
		})

		return nil
	})

	return files, err
}
