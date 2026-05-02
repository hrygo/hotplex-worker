package updater

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testUpdater(t *testing.T, handler http.HandlerFunc) (*Updater, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u := &Updater{
		CurrentVersion: "v1.3.0",
		Repo:           "test/repo",
		Client:         server.Client(),
		BaseURL:        server.URL,
		GOOS:           "darwin",
		GOARCH:         "arm64",
	}
	return u, server
}

func releaseJSON(tag string, assets []Asset) string {
	type release struct {
		TagName string  `json:"tag_name"`
		Assets  []Asset `json:"assets"`
	}
	b, _ := json.Marshal(release{TagName: tag, Assets: assets})
	return string(b)
}

func TestAssetName(t *testing.T) {
	tests := []struct {
		goos   string
		goarch string
		want   string
	}{
		{"darwin", "arm64", "hotplex-darwin-arm64"},
		{"darwin", "amd64", "hotplex-darwin-amd64"},
		{"linux", "amd64", "hotplex-linux-amd64"},
		{"linux", "arm64", "hotplex-linux-arm64"},
		{"windows", "amd64", "hotplex-windows-amd64.exe"},
		{"windows", "arm64", "hotplex-windows-arm64.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			t.Parallel()
			u := &Updater{GOOS: tt.goos, GOARCH: tt.goarch}
			require.Equal(t, tt.want, u.assetName())
		})
	}
}

func TestCheck_UpdateAvailable(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, releaseJSON("v1.4.0", []Asset{
			{Name: "hotplex-darwin-arm64", BrowserDownloadURL: "http://example.com/binary"},
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums"},
		}))
	}))
	result, err := u.Check(context.Background())
	require.NoError(t, err)
	require.True(t, result.UpdateAvailable)
	require.Equal(t, "v1.4.0", result.LatestVersion)
	require.Equal(t, "http://example.com/binary", result.DownloadURL)
	require.Equal(t, "http://example.com/checksums", result.ChecksumURL)
}

func TestCheck_AlreadyUpToDate(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, releaseJSON("v1.3.0", []Asset{
			{Name: "hotplex-darwin-arm64", BrowserDownloadURL: "http://example.com/binary"},
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums"},
		}))
	}))
	result, err := u.Check(context.Background())
	require.NoError(t, err)
	require.False(t, result.UpdateAvailable)
}

func TestCheck_RateLimited(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	_, err := u.Check(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit")
}

func TestCheck_Offline(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	u := &Updater{
		CurrentVersion: "v1.3.0",
		Repo:           "test/repo",
		Client:         server.Client(),
		BaseURL:        server.URL,
		GOOS:           "darwin",
		GOARCH:         "arm64",
	}
	_, err := u.Check(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unable to reach GitHub")
}

func TestCheck_AssetNotFound(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, releaseJSON("v1.4.0", []Asset{
			{Name: "hotplex-linux-amd64", BrowserDownloadURL: "http://example.com/binary"},
		}))
	}))
	_, err := u.Check(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no binary found")
}

func TestCheck_NonOKStatus(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err := u.Check(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}

func TestDownload_Success(t *testing.T) {
	t.Parallel()
	content := []byte("fake-binary-content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	path, err := u.Download(context.Background(), server.URL)
	require.NoError(t, err)
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, data)
}

func TestVerifyChecksum_Success(t *testing.T) {
	t.Parallel()
	// Create a temp file with known content
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "hotplex-darwin-arm64")
	content := []byte("fake-binary")
	require.NoError(t, os.WriteFile(filePath, content, 0o644))

	hash := sha256.Sum256(content)
	checksumLine := fmt.Sprintf("%s  hotplex-darwin-arm64", fmt.Sprintf("%x", hash[:]))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(checksumLine))
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	err := u.VerifyChecksum(context.Background(), server.URL, "hotplex-darwin-arm64", filePath)
	require.NoError(t, err)
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "hotplex-darwin-arm64")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0000000000000000  hotplex-darwin-arm64"))
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	err := u.VerifyChecksum(context.Background(), server.URL, "hotplex-darwin-arm64", filePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksum mismatch")
}

func TestVerifyChecksum_MissingEntry(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "hotplex-darwin-arm64")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abc123  hotplex-linux-amd64"))
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	err := u.VerifyChecksum(context.Background(), server.URL, "hotplex-darwin-arm64", filePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found in checksums.txt")
}

func TestVerifyChecksum_NoURL(t *testing.T) {
	t.Parallel()
	u := &Updater{Client: http.DefaultClient}
	err := u.VerifyChecksum(context.Background(), "", "hotplex-darwin-arm64", "/dev/null")
	require.Error(t, err)
	require.Contains(t, err.Error(), "skipping verification")
}

func TestReplace_Success(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	// Create fake "current binary"
	currentBin := filepath.Join(tmp, "hotplex")
	require.NoError(t, os.WriteFile(currentBin, []byte("old"), 0o755))

	// Create fake "new binary"
	newBin := filepath.Join(tmp, "hotplex-new")
	require.NoError(t, os.WriteFile(newBin, []byte("new"), 0o755))

	// Test the rename pattern directly
	backupPath := currentBin + ".old"
	require.NoError(t, os.Rename(currentBin, backupPath))
	require.NoError(t, os.Rename(newBin, currentBin))
	_ = os.Remove(backupPath)

	data, err := os.ReadFile(currentBin)
	require.NoError(t, err)
	require.Equal(t, []byte("new"), data)

	// New binary file should no longer exist at old path
	_, err = os.Stat(newBin)
	require.True(t, os.IsNotExist(err))
}

func TestVersionEqual(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"v1.3.0", "v1.3.0", true},
		{"1.3.0", "v1.3.0", true},
		{"v1.3.0", "1.3.0", true},
		{"v1.3.0", "v1.4.0", false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, versionEqual(tt.a, tt.b), "versionEqual(%q, %q)", tt.a, tt.b)
	}
}

func TestFindChecksum(t *testing.T) {
	t.Parallel()
	checksums := "abc123  hotplex-linux-amd64\ndef456  hotplex-darwin-arm64\n"
	hash, err := findChecksum(checksums, "hotplex-darwin-arm64")
	require.NoError(t, err)
	require.Equal(t, "def456", hash)

	_, err = findChecksum(checksums, "hotplex-windows-amd64.exe")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestIsWritable(t *testing.T) {
	t.Parallel()
	// The running test binary should be writable
	path, err := IsWritable()
	require.NoError(t, err)
	require.NotEmpty(t, path)
}

func TestIsDocker(t *testing.T) {
	t.Parallel()
	// In a test environment, IsDocker should return false (no /.dockerenv)
	require.False(t, IsDocker())
}

func TestCheck_ContextCancelled(t *testing.T) {
	t.Parallel()
	u, _ := testUpdater(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) //nolint:mnd // slow response
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_, err := u.Check(ctx)
	require.Error(t, err)
}

func TestDownload_NonOKStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	_, err := u.Download(context.Background(), server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 404")
}

func TestVerifyChecksum_HTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	u := &Updater{Client: server.Client()}
	err := u.VerifyChecksum(context.Background(), server.URL, "hotplex-darwin-arm64", "/dev/null")
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}

func TestAssetName_NonWindows(t *testing.T) {
	t.Parallel()
	u := &Updater{GOOS: "linux", GOARCH: "amd64"}
	require.Equal(t, "hotplex-linux-amd64", u.assetName())
	require.False(t, strings.HasSuffix(u.assetName(), ".exe"))
}
