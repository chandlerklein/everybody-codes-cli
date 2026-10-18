package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
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

func newUpdateCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update ec to the latest stable release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			return u.run(cmd.Context(), version, cmd.OutOrStdout())
		},
	}
}

func (u updater) run(ctx context.Context, current string, output io.Writer) error {
	if !semver.IsValid(current) {
		return fmt.Errorf("cannot update a development build (%s); install a published release from %s/releases/latest", current, updateRepository)
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
	target, err := u.executable()
	if err != nil {
		return fmt.Errorf("locate ec executable: %w", err)
	}
	if err := replaceExecutable(target, binary); err != nil {
		return fmt.Errorf("install update: %w", err)
	}
	_, err = fmt.Fprintf(output, "Updated ec from %s to %s\n", current, release.Tag)
	return err
}

func (u updater) download(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ec-updater")
	if endpoint == latestReleaseURL {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	return readUpdateBytes(resp.Body, limit)
}

func updateAssetNames(tag, goos, goarch string) (string, string, error) {
	if (goos != "darwin" && goos != "linux" && goos != "windows") || (goarch != "amd64" && goarch != "arm64") {
		return "", "", fmt.Errorf("no release binary for %s/%s", goos, goarch)
	}
	ext, binary := ".tar.gz", "ec"
	if goos == "windows" {
		ext, binary = ".zip", "ec.exe"
	}
	return fmt.Sprintf("ec_%s_%s_%s%s", strings.TrimPrefix(tag, "v"), goos, goarch, ext), binary, nil
}

func updateChecksum(checksums []byte, name string) ([]byte, error) {
	var checksum []byte
	for line := range strings.SplitSeq(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if checksum != nil {
			return nil, fmt.Errorf("duplicate checksum for %s", name)
		}
		var err error
		checksum, err = hex.DecodeString(fields[0])
		if err != nil || len(checksum) != sha256.Size {
			return nil, fmt.Errorf("invalid SHA-256 checksum for %s", name)
		}
	}
	if checksum == nil {
		return nil, fmt.Errorf("checksums.txt is missing %s", name)
	}
	return checksum, nil
}

func readUpdateBytes(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("update data exceeds %d bytes", limit)
	}
	return data, nil
}

// Read only the exact executable entry. Never extract archive paths to disk.
func unpackUpdate(archive []byte, name string) ([]byte, error) {
	var binary []byte
	if name == "ec.exe" {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			if binary != nil || !f.Mode().IsRegular() || f.UncompressedSize64 > maxUpdateBinary {
				return nil, fmt.Errorf("invalid or duplicate %s entry", name)
			}
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			binary, err = readUpdateBytes(r, maxUpdateBinary)
			r.Close()
			if err != nil {
				return nil, err
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		limited := &io.LimitedReader{R: gz, N: maxUnpackedUpdate + 1}
		tr := tar.NewReader(limited)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if h.Name != name {
				continue
			}
			if binary != nil || h.Typeflag != tar.TypeReg || h.Size > maxUpdateBinary {
				return nil, fmt.Errorf("invalid or duplicate %s entry", name)
			}
			binary, err = readUpdateBytes(tr, maxUpdateBinary)
			if err != nil {
				return nil, err
			}
		}
		// Read through the gzip trailer to verify its CRC and enforce a total size limit.
		if _, err := io.Copy(io.Discard, limited); err != nil {
			return nil, err
		}
		if limited.N == 0 {
			return nil, fmt.Errorf("unpacked update exceeds %d bytes", maxUnpackedUpdate)
		}
	}
	if len(binary) == 0 {
		return nil, fmt.Errorf("archive is missing a nonempty %s executable", name)
	}
	return binary, nil
}

func replaceExecutable(target string, binary []byte) error {
	target, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular executable file", target)
	}
	// Stage beside the executable so the final rename stays on the same filesystem.
	f, err := os.CreateTemp(filepath.Dir(target), ".ec-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to ec's installation directory; check its permissions: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(binary); err != nil {
		return err
	}
	if err := f.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return commitUpdate(f.Name(), target)
}
