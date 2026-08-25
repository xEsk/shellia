package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunUpdateReportsAvailableRelease checks the command checks releases without requiring LLM configuration.
func TestRunUpdateReportsAvailableRelease(t *testing.T) {
	archiveName := "shellia_1.2.0_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.2.0",
			"assets": []map[string]string{
				{"name": archiveName, "browser_download_url": baseURL + "/" + archiveName},
				{"name": "checksums.txt", "browser_download_url": baseURL + "/checksums.txt"},
			},
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	output, err := os.CreateTemp(dir, "output")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer output.Close()

	previousVersion := version
	SetVersion("v1.1.0")
	defer SetVersion(previousVersion)

	err = runUpdate(t.Context(), runtimeDeps{
		Stdout:           output,
		HTTPClient:       server.Client(),
		LatestReleaseURL: server.URL,
	}, config{})
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}
	contents, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(contents), "v1.2.0") || !strings.Contains(string(contents), "shellia update --yes") {
		t.Fatalf("runUpdate() output = %q, want update instructions", contents)
	}
}

// TestRunUpdateInstallsVerifiedBinary checks --yes replaces the injected executable path.
func TestRunUpdateInstallsVerifiedBinary(t *testing.T) {
	archiveName := "shellia_1.2.0_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	archive := updateTestArchive(t, []byte("new binary"))
	sum := sha256.Sum256(archive)
	checksums := hex.EncodeToString(sum[:]) + "  " + archiveName + "\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		switch r.URL.Path {
		case "/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.2.0",
				"assets": []map[string]string{
					{"name": archiveName, "browser_download_url": baseURL + "/" + archiveName},
					{"name": "checksums.txt", "browser_download_url": baseURL + "/checksums.txt"},
				},
			})
		case "/" + archiveName:
			_, _ = w.Write(archive)
		case "/checksums.txt":
			_, _ = io.WriteString(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "shellia")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	output, err := os.CreateTemp(dir, "output")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer output.Close()

	previousVersion := version
	SetVersion("v1.1.0")
	defer SetVersion(previousVersion)

	err = runUpdate(t.Context(), runtimeDeps{
		Stdout:           output,
		HTTPClient:       server.Client(),
		LatestReleaseURL: server.URL,
		ExecutablePath: func() (string, error) {
			return target, nil
		},
	}, config{UpdateYes: true})
	if err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(target) error = %v", err)
	}
	if string(contents) != "new binary" {
		t.Fatalf("target = %q, want new binary", contents)
	}
}

func updateTestArchive(t *testing.T, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "shellia", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar.Close() error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}
	return buffer.Bytes()
}
