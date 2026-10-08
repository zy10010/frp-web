package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const frpRepoLatestURL = "https://api.github.com/repos/fatedier/frp/releases/latest"

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Name    string    `json:"name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// prefixed returns the URL to fetch, optionally routing it through a mirror.
func prefixed(mirror, url string) string {
	if mirror == "" {
		return url
	}
	return strings.TrimRight(mirror, "/") + "/" + url
}

var httpClient = &http.Client{Timeout: 120 * time.Second}

func fetchRelease(mirror string) (*ghRelease, error) {
	rel, err := fetchReleaseFrom(prefixed(mirror, frpRepoLatestURL))
	if err != nil && mirror != "" {
		// Best-effort mirror: fall back to a direct request on failure.
		return fetchReleaseFrom(frpRepoLatestURL)
	}
	return rel, err
}

func fetchReleaseFrom(url string) (*ghRelease, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// trimV strips a leading "v" from a version tag.
func trimV(v string) string {
	return strings.TrimPrefix(v, "v")
}

func localBinaryVersion(binPath string) string {
	if binPath == "" {
		return ""
	}
	if _, err := os.Stat(binPath); err != nil {
		return ""
	}
	out, err := readOutput(binPath, "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
}

// assetSuffix maps runtime GOOS/GOARCH to the frp release asset suffix.
func assetSuffix(goos, goarch string) (string, error) {
	if goarch == "arm" {
		goarm := os.Getenv("GOARM")
		n := 7
		if goarm != "" {
			if v, err := strconv.Atoi(goarm); err == nil {
				n = v
			}
		}
		if n >= 7 {
			return "linux_arm_hf", nil
		}
		return "linux_arm", nil
	}
	switch goarch {
	case "amd64", "arm64", "386", "mips", "mipsle", "mips64", "mips64le", "riscv64", "loong64":
		return goos + "_" + goarch, nil
	}
	return "", fmt.Errorf("unsupported architecture %s", goarch)
}

func assetExt(goos string) string {
	if goos == "windows" {
		return "zip"
	}
	return "tar.gz"
}

// findAsset returns the release asset matching the current platform.
func findAsset(rel *ghRelease) (*ghAsset, string, error) {
	ver := trimV(rel.TagName)
	suffix, err := assetSuffix(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, ver, err
	}
	name := "frp_" + ver + "_" + suffix + "." + assetExt(runtime.GOOS)
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i], ver, nil
		}
	}
	return nil, ver, fmt.Errorf("no release asset for platform %s_%s (%s)", runtime.GOOS, runtime.GOARCH, name)
}

// downloadToFile downloads url to dst, returning an error on any failure.
func downloadToFile(url, dst string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// extractRelease unpacks a .tar.gz or .zip archive into dstDir.
func extractRelease(archive, dstDir string) error {
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(archive, dstDir)
	}
	return extractTarGz(archive, dstDir)
}

// maxExtractedFileSize caps the size of any single file extracted from the
// release archive, guarding against decompression bombs.
const maxExtractedFileSize = 1 << 30 // 1 GiB

// safeExtractTarget joins dstDir and name, rejecting any path that would escape
// dstDir (e.g. via "../" in a malicious archive).
func safeExtractTarget(dstDir, name string) (string, error) {
	target := filepath.Join(dstDir, name)
	rel, err := filepath.Rel(dstDir, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal path in archive: %s", name)
	}
	return target, nil
}

func extractTarGz(archive, dstDir string) error {
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
		target, err := safeExtractTarget(dstDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			written, err := io.Copy(out, io.LimitReader(tr, maxExtractedFileSize+1))
			if err != nil {
				out.Close()
				return err
			}
			if written > maxExtractedFileSize {
				out.Close()
				return fmt.Errorf("file too large in archive: %s", hdr.Name)
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractZip(archive, dstDir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		target, err := safeExtractTarget(dstDir, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			src.Close()
			return err
		}
		written, err := io.Copy(out, io.LimitReader(src, maxExtractedFileSize+1))
		if err != nil {
			out.Close()
			src.Close()
			return err
		}
		if written > maxExtractedFileSize {
			out.Close()
			src.Close()
			return fmt.Errorf("file too large in archive: %s", zf.Name)
		}
		if err := out.Close(); err != nil {
			src.Close()
			return err
		}
		src.Close()
	}
	return nil
}

// applyUpdateResult carries the outcome of an update operation.
type applyUpdateResult struct {
	Message string `json:"message"`
}

// applyUpdate downloads and installs the given release, replacing the local
// frps/frpc binaries and restarting whichever processes were running.
func (m *Manager) applyUpdate(rel *ghRelease) (applyUpdateResult, error) {
	asset, ver, err := findAsset(rel)
	if err != nil {
		return applyUpdateResult{}, err
	}

	tmpDir, err := os.MkdirTemp("", "frp-update-")
	if err != nil {
		return applyUpdateResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	archive := filepath.Join(tmpDir, asset.Name)
	downloadURL := prefixed(m.cfg.Mirror, asset.BrowserDownloadURL)
	if err := downloadToFile(downloadURL, archive); err != nil {
		// Fall back to direct download if the mirror failed.
		if m.cfg.Mirror != "" {
			if err2 := downloadToFile(asset.BrowserDownloadURL, archive); err2 != nil {
				return applyUpdateResult{}, fmt.Errorf("download via mirror and direct both failed: %v / %v", err, err2)
			}
		} else {
			return applyUpdateResult{}, err
		}
	}

	extractDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return applyUpdateResult{}, err
	}
	if err := extractRelease(archive, extractDir); err != nil {
		return applyUpdateResult{}, err
	}

	// The release extracts to a single top-level directory named
	// frp_<ver>_<os>_<arch>.
	root := findContainingDir(extractDir, "frps")
	if root == "" {
		return applyUpdateResult{}, fmt.Errorf("frps binary not found in release archive")
	}
	newFrps := filepath.Join(root, "frps")
	newFrpc := filepath.Join(root, "frpc")

	wasFrps := m.frps.Status().Running
	wasFrpc := m.frpc.Status().Running
	m.frps.Stop()
	m.frpc.Stop()

	if err := replaceBinary(newFrps, m.cfg.FrpsPath); err != nil {
		m.restartAfterUpdate(wasFrps, wasFrpc)
		return applyUpdateResult{}, err
	}
	if err := replaceBinary(newFrpc, m.cfg.FrpcPath); err != nil {
		m.restartAfterUpdate(wasFrps, wasFrpc)
		return applyUpdateResult{}, err
	}

	m.restartAfterUpdate(wasFrps, wasFrpc)
	return applyUpdateResult{Message: fmt.Sprintf("Updated frp to v%s", ver)}, nil
}

func (m *Manager) restartAfterUpdate(frpsWasRunning, frpcWasRunning bool) {
	if frpsWasRunning {
		_ = m.frps.Start()
	}
	if frpcWasRunning {
		_ = m.frpc.Start()
	}
}

// replaceBinary atomically replaces dst with src, keeping a .bak copy.
func replaceBinary(src, dst string) error {
	if dst == "" {
		return fmt.Errorf("target binary path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	bak := dst + ".bak"
	_ = os.Remove(bak)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, bak); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	// 0755 is required: this writes the frps/frpc executable binary.
	//nolint:gosec
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		_ = os.Rename(bak, dst) // restore on failure
		return err
	}
	return nil
}

// findContainingDir walks dir looking for a file named name, returning its
// parent directory.
func findContainingDir(dir, name string) string {
	var found string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !info.IsDir() && info.Name() == name {
			found = filepath.Dir(path)
		}
		return nil
	})
	return found
}
