// Package updater provides self-update functionality for the hotplex binary.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultRepo = "hrygo/hotplex"
const defaultBaseURL = "https://api.github.com"

// Release represents a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a single release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckResult is returned by Check.
type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	AssetName       string
	DownloadURL     string
	ChecksumURL     string
}

// Updater holds configuration for update operations.
type Updater struct {
	CurrentVersion string
	Repo           string
	Client         *http.Client
	BaseURL        string
	GOOS           string
	GOARCH         string
}

// New creates an Updater with production defaults.
func New(currentVersion string) *Updater {
	return &Updater{
		CurrentVersion: currentVersion,
		Repo:           defaultRepo,
		Client:         &http.Client{Timeout: 30 * 1e9}, // 30s
		BaseURL:        defaultBaseURL,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
	}
}

// assetName returns the expected binary name for the current platform.
func (u *Updater) assetName() string {
	name := fmt.Sprintf("hotplex-%s-%s", u.GOOS, u.GOARCH)
	if u.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Check queries the GitHub API for the latest release.
func (u *Updater) Check(ctx context.Context) (*CheckResult, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", u.BaseURL, u.Repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to reach GitHub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusForbidden:
		return nil, fmt.Errorf("GitHub API rate limit exceeded. Try again later")
	case http.StatusNotFound:
		return nil, fmt.Errorf("no releases found for %s", u.Repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return nil, fmt.Errorf("parse release info: %w", err)
	}

	want := u.assetName()
	var downloadURL, checksumURL string
	for _, a := range release.Assets {
		switch a.Name {
		case want:
			downloadURL = a.BrowserDownloadURL
		case "checksums.txt":
			checksumURL = a.BrowserDownloadURL
		}
	}
	if downloadURL == "" {
		return nil, fmt.Errorf("no binary found for %s in release %s", want, release.TagName)
	}

	return &CheckResult{
		CurrentVersion:  u.CurrentVersion,
		LatestVersion:   release.TagName,
		UpdateAvailable: !versionEqual(u.CurrentVersion, release.TagName),
		AssetName:       want,
		DownloadURL:     downloadURL,
		ChecksumURL:     checksumURL,
	}, nil
}

// Download fetches the binary to a temp file and returns its path.
// Caller is responsible for cleaning up the temp file.
func (u *Updater) Download(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Use a longer timeout for binary download.
	dlClient := &http.Client{Timeout: 120 * 1e9} // 120s
	resp, err := dlClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download binary: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "hotplex-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 200<<20)); err != nil { // 200MB max
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	return tmpPath, nil
}

// VerifyChecksum downloads checksums.txt and compares sha256 of the file at path.
func (u *Updater) VerifyChecksum(ctx context.Context, checksumURL, assetName, filePath string) error {
	if checksumURL == "" {
		return fmt.Errorf("checksums.txt not available — skipping verification is unsafe")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("create checksum request: %w", err)
	}

	resp, err := u.Client.Do(req)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download checksums failed with HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10)) // 64KB max
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}

	expected, err := findChecksum(string(data), assetName)
	if err != nil {
		return err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open downloaded file: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))

	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

// Replace atomically replaces the current binary with the new one.
func (u *Updater) Replace(newBinaryPath string) error {
	currentExe, err := executablePath()
	if err != nil {
		return err
	}

	// Ensure the new binary is executable.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(newBinaryPath, 0o755); err != nil {
			return fmt.Errorf("chmod new binary: %w", err)
		}
	}

	// Step 1: rename current binary to .old
	backupPath := currentExe + ".old"
	_ = os.Remove(backupPath)
	if err := os.Rename(currentExe, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w\n  Try: sudo hotplex update", err)
	}

	// Step 2: rename new binary into place
	if err := os.Rename(newBinaryPath, currentExe); err != nil {
		// Rollback
		_ = os.Rename(backupPath, currentExe)
		return fmt.Errorf("install new binary: %w", err)
	}

	// Step 3: cleanup backup (best-effort)
	_ = os.Remove(backupPath)

	return nil
}

// IsWritable checks if the current binary path is writable, returns the resolved path.
func IsWritable() (string, error) {
	exe, err := executablePath()
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(exe, os.O_WRONLY, 0)
	if err != nil {
		return exe, fmt.Errorf("no write permission for %s: %w\n  Try: sudo hotplex update", exe, err)
	}
	_ = f.Close()
	return exe, nil
}

// IsDocker checks if running inside a Docker container.
func IsDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "docker") || strings.Contains(string(data), "containerd")
}

// executablePath returns the resolved path of the running binary.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve binary path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("eval symlinks: %w", err)
	}
	return resolved, nil
}

// findChecksum parses sha256sum output format: "{hash}  {filename}".
func findChecksum(checksums, filename string) (string, error) {
	for _, line := range strings.Split(checksums, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == filename {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("%s not found in checksums.txt", filename)
}

// versionEqual compares two version strings, normalizing "v" prefix.
func versionEqual(a, b string) bool {
	return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}
