// Package update installs verified GitHub releases of ec.
package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"time"

	"golang.org/x/mod/semver"
)

const (
	updateRepository  = "https://github.com/chandlerklein/everybody-codes-cli"
	latestReleaseURL  = "https://api.github.com/repos/chandlerklein/everybody-codes-cli/releases/latest"
	maxUpdateMetadata = 1 << 20
	maxUpdateArchive  = 64 << 20
	maxUpdateBinary   = 64 << 20
	maxUnpackedUpdate = 128 << 20
)

type updateRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

type updater struct {
	client       *http.Client
	executable   func() (string, error)
	goos, goarch string
}

// Run installs the latest stable release when it is newer than current.
func Run(ctx context.Context, current string, output io.Writer) error {
	u := updater{
		client: &http.Client{
			Timeout: 2 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "https" {
					return fmt.Errorf("update redirect must use HTTPS")
				}
				if len(via) >= 10 {
					return fmt.Errorf("too many update redirects")
				}
				return nil
			},
		},
		executable: os.Executable,
		goos:       runtime.GOOS, goarch: runtime.GOARCH,
	}
	return u.run(ctx, current, output)
}

func (u updater) run(ctx context.Context, current string, output io.Writer) error {
	if !semver.IsValid(current) {
		return fmt.Errorf("cannot update a development build (%s); install a published release from %s/releases/latest", current, updateRepository)
	}
	target, err := u.executable()
	if err != nil {
		return fmt.Errorf("locate ec executable: %w", err)
	}
	if u.goos == "darwin" && homebrewExecutable(target) {
		return fmt.Errorf("ec is managed by Homebrew; run `brew update && brew upgrade --cask chandlerklein/tap/ec`")
	}
	metadata, err := u.download(ctx, latestReleaseURL, maxUpdateMetadata)
	if err != nil {
		return fmt.Errorf("check latest release: %w", err)
	}
	var release updateRelease
	if err := json.Unmarshal(metadata, &release); err != nil {
		return fmt.Errorf("read latest release: %w", err)
	}
	if release.Draft || release.Prerelease || !semver.IsValid(release.Tag) || semver.Prerelease(release.Tag) != "" {
		return fmt.Errorf("GitHub did not return a valid stable release")
	}
	if semver.Compare(current, release.Tag) >= 0 {
		_, err := fmt.Fprintln(output, "ec is up to date")
		return err
	}
	archiveName, binaryName, err := updateAssetNames(release.Tag, u.goos, u.goarch)
	if err != nil {
		return err
	}
	for _, name := range []string{archiveName, "checksums.txt"} {
		found := false
		for _, asset := range release.Assets {
			if asset.Name == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("release %s is missing %s", release.Tag, name)
		}
	}
	// Construct URLs for this repository rather than trusting arbitrary asset URLs.
	assetBase := updateRepository + "/releases/download/" + url.PathEscape(release.Tag) + "/"
	checksums, err := u.download(ctx, assetBase+"checksums.txt", maxUpdateMetadata)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	wantHash, err := updateChecksum(checksums, archiveName)
	if err != nil {
		return err
	}
	archive, err := u.download(ctx, assetBase+url.PathEscape(archiveName), maxUpdateArchive)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	gotHash := sha256.Sum256(archive)
	if !bytes.Equal(gotHash[:], wantHash) {
		return fmt.Errorf("checksum verification failed for %s; ec was not changed", archiveName)
	}
	binary, err := unpackUpdate(archive, binaryName)
	if err != nil {
		return fmt.Errorf("unpack update: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := replaceExecutable(target, binary); err != nil {
		return fmt.Errorf("install update: %w", err)
	}
	_, err = fmt.Fprintf(output, "Updated ec from %s to %s\n", current, release.Tag)
	return err
}
