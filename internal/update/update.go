// Package update checks GitHub releases and safely installs a verified Shellia binary.
package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrPermission reports that replacing the current executable needs administrator privileges.
var ErrPermission = errors.New("administrator permissions are required to replace the current executable")

const (
	// LatestReleaseURL is the GitHub API endpoint for Shellia's latest release.
	LatestReleaseURL = "https://api.github.com/repos/xEsk/shellia/releases/latest"

	checksumAssetName = "checksums.txt"
	maxDownloadBytes  = 100 * 1024 * 1024
)

// Release contains the verified-download metadata for one Shellia release.
type Release struct {
	Tag         string
	ArchiveURL  string
	ChecksumURL string
}

// CheckResult describes whether a newer compatible release is available.
type CheckResult struct {
	Available bool
	Release   Release
}

type releaseResponse struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// ArchiveName returns the GoReleaser archive name for a supported platform.
func ArchiveName(tag, goos, goarch string) (string, error) {
	if goos != "darwin" && goos != "linux" {
		return "", fmt.Errorf("automatic updates are not supported on %s", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("automatic updates are not supported on %s/%s", goos, goarch)
	}
	version := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if version == "" {
		return "", errors.New("release has no version tag")
	}
	return fmt.Sprintf("shellia_%s_%s_%s.tar.gz", version, goos, goarch), nil
}

// CheckLatest checks GitHub for a newer release compatible with this binary's platform.
func CheckLatest(ctx context.Context, client *http.Client, latestURL, currentVersion string) (CheckResult, error) {
	if client == nil {
		client = http.DefaultClient
	}

	response, err := get(ctx, client, latestURL)
	if err != nil {
		return CheckResult{}, fmt.Errorf("cannot check for updates: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return CheckResult{}, fmt.Errorf("cannot check for updates: GitHub returned %s", response.Status)
	}

	var latest releaseResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&latest); err != nil {
		return CheckResult{}, fmt.Errorf("cannot read latest release: %w", err)
	}

	release := Release{Tag: strings.TrimSpace(latest.TagName)}
	if release.Tag == "" {
		return CheckResult{}, errors.New("latest release has no version tag")
	}
	if !isNewer(release.Tag, currentVersion) {
		return CheckResult{Release: release}, nil
	}

	archiveName, err := ArchiveName(latest.TagName, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return CheckResult{}, err
	}

	for _, candidate := range latest.Assets {
		switch candidate.Name {
		case archiveName:
			release.ArchiveURL = candidate.URL
		case checksumAssetName:
			release.ChecksumURL = candidate.URL
		}
	}
	if release.ArchiveURL == "" {
		return CheckResult{}, fmt.Errorf("latest release %s has no downloadable archive for %s/%s", release.Tag, runtime.GOOS, runtime.GOARCH)
	}
	if release.ChecksumURL == "" {
		return CheckResult{}, fmt.Errorf("latest release %s has no %s", release.Tag, checksumAssetName)
	}

	return CheckResult{Available: true, Release: release}, nil
}

// DownloadBinary downloads, verifies and extracts a Shellia binary from a release.
func DownloadBinary(ctx context.Context, client *http.Client, release Release) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	archivePath, archiveSum, err := download(ctx, client, release.ArchiveURL)
	if err != nil {
		return "", fmt.Errorf("cannot download %s: %w", release.Tag, err)
	}
	defer os.Remove(archivePath)

	checksums, err := downloadText(ctx, client, release.ChecksumURL)
	if err != nil {
		return "", fmt.Errorf("cannot download release checksums: %w", err)
	}
	archiveName, err := ArchiveName(release.Tag, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	want, ok := checksumFor(checksums, archiveName)
	if !ok {
		return "", fmt.Errorf("release checksums do not include %s", archiveName)
	}
	if !strings.EqualFold(want, hex.EncodeToString(archiveSum[:])) {
		return "", errors.New("download checksum does not match the release manifest")
	}

	path, err := extractBinary(archivePath)
	if err != nil {
		return "", fmt.Errorf("cannot extract updated binary: %w", err)
	}
	return path, nil
}

// Install atomically replaces target with stagedBinary when the current process can write it.
func Install(stagedBinary, target string) error {
	target, err := resolvedExecutable(target)
	if err != nil {
		return err
	}

	if err := installDirect(stagedBinary, target); err != nil {
		if os.IsPermission(err) {
			return ErrPermission
		}
		return err
	}
	return nil
}

func get(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "shellia-update")
	return client.Do(req)
}

func download(ctx context.Context, client *http.Client, rawURL string) (string, [sha256.Size]byte, error) {
	response, err := get(ctx, client, rawURL)
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", [sha256.Size]byte{}, fmt.Errorf("server returned %s", response.Status)
	}
	if response.ContentLength > maxDownloadBytes {
		return "", [sha256.Size]byte{}, fmt.Errorf("download exceeds %d bytes", maxDownloadBytes)
	}

	file, err := os.CreateTemp("", "shellia-update-*.tar.gz")
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()

	hash := sha256.New()
	reader := io.LimitReader(response.Body, maxDownloadBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(file, hash), reader)
	closeErr := file.Close()
	if copyErr != nil {
		return "", [sha256.Size]byte{}, copyErr
	}
	if closeErr != nil {
		return "", [sha256.Size]byte{}, closeErr
	}
	if written > maxDownloadBytes {
		return "", [sha256.Size]byte{}, fmt.Errorf("download exceeds %d bytes", maxDownloadBytes)
	}

	var sum [sha256.Size]byte
	copy(sum[:], hash.Sum(nil))
	keep = true
	return path, sum, nil
}

func downloadText(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	response, err := get(ctx, client, rawURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %s", response.Status)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil {
		return "", err
	}
	if len(contents) > 1024*1024 {
		return "", errors.New("checksum manifest is too large")
	}
	return string(contents), nil
}

func checksumFor(manifest, name string) (string, bool) {
	for _, line := range strings.Split(manifest, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if _, err := hex.DecodeString(fields[0]); err == nil && len(fields[0]) == sha256.Size*2 {
			return fields[0], true
		}
	}
	return "", false
}

func extractBinary(archivePath string) (string, error) {
	archive, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer archive.Close()

	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag != tar.TypeReg || strings.TrimPrefix(filepath.Clean(header.Name), "."+string(filepath.Separator)) != "shellia" {
			continue
		}
		if header.Size < 1 || header.Size > maxDownloadBytes {
			return "", errors.New("invalid binary size in release archive")
		}

		binary, err := os.CreateTemp("", "shellia-update-*")
		if err != nil {
			return "", err
		}
		path := binary.Name()
		written, copyErr := io.Copy(binary, io.LimitReader(reader, maxDownloadBytes+1))
		closeErr := binary.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			_ = os.Remove(path)
			if copyErr != nil {
				return "", copyErr
			}
			if closeErr != nil {
				return "", closeErr
			}
			return "", errors.New("release archive has a truncated binary")
		}
		if err := os.Chmod(path, 0o755); err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return path, nil
	}
	return "", errors.New("release archive does not contain the shellia binary")
}

func isNewer(latest, current string) bool {
	latest = strings.TrimPrefix(strings.TrimSpace(latest), "v")
	current = strings.TrimPrefix(strings.TrimSpace(current), "v")
	if latest == "" || current == "" || current == "dev" {
		return latest != ""
	}

	latestParts, latestOK := numericVersion(latest)
	currentParts, currentOK := numericVersion(current)
	if !latestOK || !currentOK {
		return latest != current
	}
	for index := range latestParts {
		if latestParts[index] != currentParts[index] {
			return latestParts[index] > currentParts[index]
		}
	}
	return false
}

func numericVersion(value string) ([3]int, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var parsed [3]int
	for index, part := range parts {
		if part == "" || strings.ContainsAny(part, "+-") {
			return [3]int{}, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return [3]int{}, false
		}
		parsed[index] = value
	}
	return parsed, true
}

func resolvedExecutable(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("cannot determine the current executable path")
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err == nil {
		target = resolved
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("current executable %s is not a regular file", target)
	}
	return target, nil
}

func installDirect(source, target string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	temporary, err := os.CreateTemp(filepath.Dir(target), ".shellia-update-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := io.Copy(temporary, sourceFile); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o755); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
}
