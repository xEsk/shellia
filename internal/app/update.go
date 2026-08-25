package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	updatepkg "github.com/xEsk/shellia/internal/update"
)

const updateTimeout = time.Minute

// runUpdate checks for a compatible release and optionally installs it in place.
func runUpdate(ctx context.Context, deps runtimeDeps, cfg config) error {
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	result, err := updatepkg.CheckLatest(ctx, deps.HTTPClient, deps.LatestReleaseURL, version)
	if err != nil {
		return err
	}
	if !result.Available {
		fmt.Fprintf(deps.Stdout, "Shellia %s is already up to date.\n", version)
		return nil
	}

	fmt.Fprintf(deps.Stdout, "A new Shellia release is available: %s (current: %s).\n", result.Release.Tag, version)
	if !cfg.UpdateYes {
		fmt.Fprintln(deps.Stdout, "Run `shellia update --yes` to download and install it.")
		return nil
	}

	stagedBinary, err := updatepkg.DownloadBinary(ctx, deps.HTTPClient, result.Release)
	if err != nil {
		return err
	}
	defer os.Remove(stagedBinary)

	target, err := deps.ExecutablePath()
	if err != nil {
		return fmt.Errorf("cannot determine the current executable: %w", err)
	}
	err = updatepkg.Install(stagedBinary, target)
	if err != nil {
		if errors.Is(err, updatepkg.ErrPermission) {
			return fmt.Errorf("%w; run `sudo shellia update --yes`", err)
		}
		return err
	}
	fmt.Fprintln(deps.Stdout, "Shellia has been updated.")
	return nil
}
