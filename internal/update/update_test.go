package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestArchiveNameMatchesGoReleaserVersion(t *testing.T) {
	got, err := ArchiveName("v0.2.2", "linux", "amd64")
	if err != nil {
		t.Fatalf("ArchiveName() error = %v", err)
	}
	const want = "shellia_0.2.2_linux_amd64.tar.gz"
	if got != want {
		t.Fatalf("ArchiveName() = %q, want %q", got, want)
	}
}

func TestCheckLatestFindsCompatibleRelease(t *testing.T) {
	archiveName, err := ArchiveName("v1.2.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Fatal("request did not set a User-Agent")
		}
		_ = json.NewEncoder(w).Encode(releaseResponse{
			TagName: "v1.2.0",
			Assets: []asset{
				{Name: archiveName, URL: "https://example.test/" + archiveName},
				{Name: checksumAssetName, URL: "https://example.test/checksums.txt"},
			},
		})
	}))
	defer server.Close()

	result, err := CheckLatest(t.Context(), server.Client(), server.URL, "v1.1.0")
	if err != nil {
		t.Fatalf("CheckLatest() error = %v", err)
	}
	if !result.Available || result.Release.Tag != "v1.2.0" {
		t.Fatalf("CheckLatest() = %#v, want available v1.2.0", result)
	}
}

func TestCheckLatestCurrentVersionDoesNotRequireAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseResponse{TagName: "v0.2.2"})
	}))
	defer server.Close()

	result, err := CheckLatest(t.Context(), server.Client(), server.URL, "v0.2.2")
	if err != nil {
		t.Fatalf("CheckLatest() error = %v", err)
	}
	if result.Available {
		t.Fatalf("CheckLatest() Available = true, want false")
	}
}

func TestCheckLatestRejectsReleaseWithoutCurrentPlatformArchive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseResponse{
			TagName: "v1.2.0",
			Assets:  []asset{{Name: checksumAssetName, URL: "https://example.test/checksums.txt"}},
		})
	}))
	defer server.Close()

	_, err := CheckLatest(t.Context(), server.Client(), server.URL, "v1.1.0")
	if err == nil || !strings.Contains(err.Error(), "no downloadable archive") {
		t.Fatalf("CheckLatest() error = %v, want missing archive", err)
	}
}

func TestDownloadBinaryVerifiesChecksumAndExtractsShellia(t *testing.T) {
	archiveName, err := ArchiveName("v1.2.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	archive := testArchive(t, "shellia", []byte("new binary"))
	sum := sha256.Sum256(archive)
	checksums := hex.EncodeToString(sum[:]) + "  " + archiveName + "\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + archiveName:
			_, _ = w.Write(archive)
		case "/checksums.txt":
			_, _ = io.WriteString(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	path, err := DownloadBinary(t.Context(), server.Client(), Release{
		Tag:         "v1.2.0",
		ArchiveURL:  server.URL + "/" + archiveName,
		ChecksumURL: server.URL + "/checksums.txt",
	})
	if err != nil {
		t.Fatalf("DownloadBinary() error = %v", err)
	}
	defer os.Remove(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "new binary" {
		t.Fatalf("binary = %q, want new binary", got)
	}
}

func TestDownloadBinaryRejectsChecksumMismatch(t *testing.T) {
	archiveName, err := ArchiveName("v1.2.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	archive := testArchive(t, "shellia", []byte("new binary"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + archiveName:
			_, _ = w.Write(archive)
		case "/checksums.txt":
			_, _ = io.WriteString(w, strings.Repeat("0", 64)+"  "+archiveName+"\n")
		}
	}))
	defer server.Close()

	_, err = DownloadBinary(t.Context(), server.Client(), Release{
		Tag:         "v1.2.0",
		ArchiveURL:  server.URL + "/" + archiveName,
		ChecksumURL: server.URL + "/checksums.txt",
	})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("DownloadBinary() error = %v, want checksum mismatch", err)
	}
}

func TestInstallReplacesWritableExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shellia")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatalf("WriteFile(staged) error = %v", err)
	}

	err := Install(staged, target)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(target) error = %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("target = %q, want new", got)
	}
}

func TestInstallReportsProtectedDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write protected test directories")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "shellia")
	staged := filepath.Join(t.TempDir(), "staged")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatalf("WriteFile(staged) error = %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	defer os.Chmod(dir, 0o755) //nolint:errcheck // Required for temporary-directory cleanup.

	err := Install(staged, target)
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("Install() error = %v, want ErrPermission", err)
	}
}

func TestIsNewerComparesReleaseVersions(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{latest: "v1.2.0", current: "v1.1.9", want: true},
		{latest: "v1.2.0", current: "v1.2.0", want: false},
		{latest: "v1.2.0", current: "v1.3.0", want: false},
		{latest: "v1.2.0", current: "dev", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.latest+"/"+tt.current, func(t *testing.T) {
			if got := isNewer(tt.latest, tt.current); got != tt.want {
				t.Fatalf("isNewer(%q, %q) = %t, want %t", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func testArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
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
