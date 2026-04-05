package transfer

import (
	"os"
	"path/filepath"
	"strings"
)

// MediaExtensions is the list of file extensions to copy.
var MediaExtensions = map[string]bool{
	// Video
	".mp4": true, ".mov": true, ".avi": true, ".mts": true,
	".m2ts": true, ".mkv": true, ".wmv": true, ".3gp": true,
	// Photo
	".jpg": true, ".jpeg": true, ".png": true, ".tiff": true,
	".tif": true, ".heic": true, ".heif": true,
	// Raw
	".cr2": true, ".cr3": true, ".arw": true, ".nef": true,
	".dng": true, ".raw": true, ".orf": true, ".rw2": true,
	".raf": true, ".srw": true,
	// Pro
	".braw": true, ".r3d": true, ".prores": true, ".mxf": true,
	// Audio
	".wav": true, ".mp3": true, ".aac": true, ".m4a": true,
}

// MediaFile represents a discovered media file on a source card.
type MediaFile struct {
	AbsPath string // Full path on disk
	RelPath string // Path relative to the card root
	Size    int64  // File size in bytes
}

// IsMediaFile returns true if the filename has a recognized media extension.
func IsMediaFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return MediaExtensions[ext]
}

// DiscoverMediaFiles walks a directory tree and returns all media files.
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

		if !IsMediaFile(info.Name()) {
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
