package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/ekkolyth/dump/internal/version"
)

const releaseAPI = "https://api.github.com/repos/ekkolyth/dump/releases/latest"

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func Run() error {
	fmt.Println("Checking for updates...")

	latest, err := fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	if latest == version.Version {
		fmt.Printf("Already up to date (v%s)\n", version.Version)
		return nil
	}

	fmt.Printf("Upgrading v%s → v%s\n", version.Version, latest)

	asset := assetName(latest, runtime.GOOS, runtime.GOARCH)
	url := fmt.Sprintf("https://github.com/ekkolyth/dump/releases/download/v%s/%s", latest, asset)

	tmpDir, err := os.MkdirTemp("", "dump-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, asset)
	if err := download(url, archivePath); err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	binaryName := "dump"
	if runtime.GOOS == "windows" {
		binaryName = "dump.exe"
	}

	extractedPath := filepath.Join(tmpDir, binaryName)
	if strings.HasSuffix(asset, ".zip") {
		if err := extractZip(archivePath, tmpDir, binaryName); err != nil {
			return fmt.Errorf("failed to extract: %w", err)
		}
	} else {
		if err := extractTarGz(archivePath, tmpDir, binaryName); err != nil {
			return fmt.Errorf("failed to extract: %w", err)
		}
	}

	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find current binary: %w", err)
	}
	currentBinary, err = filepath.EvalSymlinks(currentBinary)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	if err := replaceBinary(extractedPath, currentBinary); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Printf("Successfully upgraded to v%s\n", latest)
	return nil
}

func fetchLatestVersion() (string, error) {
	resp, err := http.Get(releaseAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return parseVersionTag(release.TagName), nil
}

func parseVersionTag(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

func assetName(ver, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	suffix := ""
	if goos == "darwin" {
		suffix = macosSuffix()
	}
	return fmt.Sprintf("dump_%s_%s_%s%s.%s", ver, goos, goarch, suffix, ext)
}

func macosSuffix() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	major, err := strconv.Atoi(strings.Split(strings.TrimSpace(string(out)), ".")[0])
	if err != nil {
		return ""
	}
	if major < 12 {
		return "_macos11"
	}
	return ""
}

func download(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func extractTarGz(archive, destDir, binaryName string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == binaryName {
			out, err := os.OpenFile(filepath.Join(destDir, binaryName), os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, tr)
			return err
		}
	}
	return fmt.Errorf("binary %s not found in archive", binaryName)
}

func extractZip(archive, destDir, binaryName string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == binaryName {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			out, err := os.OpenFile(filepath.Join(destDir, binaryName), os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, rc)
			return err
		}
	}
	return fmt.Errorf("binary %s not found in archive", binaryName)
}

func replaceBinary(newPath, currentPath string) error {
	if err := os.Rename(newPath, currentPath); err != nil {
		// Cross-device fallback: copy then rename
		tmp := currentPath + ".new"
		src, err := os.Open(newPath)
		if err != nil {
			return err
		}
		defer src.Close()

		dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY, 0755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			return err
		}
		dst.Close()

		return os.Rename(tmp, currentPath)
	}
	return nil
}
