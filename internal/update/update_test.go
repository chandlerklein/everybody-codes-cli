package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUpdateInstallsVerifiedRelease(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			f := setupUpdate(t, goos, []byte("new executable"))
			before, err := os.Stat(f.target)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := f.u.run(t.Context(), "v0.1.0", &output); err != nil {
				t.Fatal(err)
			}
			assertFile(t, f.target, "new executable")
			after, err := os.Stat(f.target)
			if err != nil {
				t.Fatal(err)
			}
			if after.Mode().Perm() != before.Mode().Perm() {
				t.Errorf("mode = %v, want %v", after.Mode(), before.Mode())
			}
			if got, want := output.String(), "Updated ec from v0.1.0 to v0.2.0\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
			wantRequests := []string{latestReleaseURL, f.base + "checksums.txt", f.base + f.archiveName}
			if !slices.Equal(f.requests, wantRequests) {
				t.Errorf("requests = %v, want %v", f.requests, wantRequests)
			}
			entries, err := os.ReadDir(filepath.Dir(f.target))
			if err != nil || len(entries) != 1 {
				t.Errorf("update left temporary files: %v, %v", entries, err)
			}
		})
	}
}

func TestUpdateDoesNotDowngradeOrReinstall(t *testing.T) {
	for _, current := range []string{"v0.2.0", "v0.2.0+build.1", "v0.3.0", "v0.10.0", "v1.0.0-rc.1"} {
		t.Run(current, func(t *testing.T) {
			f := setupUpdate(t, "linux", []byte("new executable"))
			var output bytes.Buffer
			if err := f.u.run(t.Context(), current, &output); err != nil {
				t.Fatal(err)
			}
			if output.String() != "ec is up to date\n" {
				t.Errorf("output = %q", output.String())
			}
			assertFile(t, f.target, "old executable")
			if !slices.Equal(f.requests, []string{latestReleaseURL}) {
				t.Errorf("downloaded assets unnecessarily: %v", f.requests)
			}
		})
	}
}

func TestUpdateUpgradesPrerelease(t *testing.T) {
	f := setupUpdate(t, "linux", []byte("new executable"))
	var output bytes.Buffer
	if err := f.u.run(t.Context(), "v0.2.0-rc.1", &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Updated ec from v0.2.0-rc.1 to v0.2.0\n" {
		t.Fatalf("output = %q", output.String())
	}
}

func TestUpdateHomebrewInstallation(t *testing.T) {
	for _, current := range []string{"v0.1.0", testUpdateTag} {
		t.Run(current, func(t *testing.T) {
			f := setupUpdate(t, "darwin", []byte("new executable"))
			f.target = filepath.Join(t.TempDir(), "custom-prefix", "Caskroom", "ec", "0.1.0", "ec")
			if err := os.MkdirAll(filepath.Dir(f.target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.target, []byte("old executable"), 0o755); err != nil {
				t.Fatal(err)
			}
			err := f.u.run(t.Context(), current, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "brew upgrade --cask chandlerklein/tap/ec") {
				t.Fatalf("error = %v", err)
			}
			if len(f.requests) != 0 {
				t.Fatalf("Homebrew update made requests: %v", f.requests)
			}
			assertFile(t, f.target, "old executable")
		})
	}
}

func TestUpdateFailureKeepsExecutable(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*updateFixture)
		want   string
	}{
		{"rate limited", func(f *updateFixture) { f.statuses[latestReleaseURL] = http.StatusForbidden }, "HTTP 403"},
		{"no releases", func(f *updateFixture) { f.statuses[latestReleaseURL] = http.StatusNotFound }, "HTTP 404"},
		{"server failure", func(f *updateFixture) { f.statuses[latestReleaseURL] = http.StatusBadGateway }, "HTTP 502"},
		{"invalid JSON", func(f *updateFixture) { f.bodies[latestReleaseURL] = []byte("{") }, "read latest release"},
		{"invalid tag", func(f *updateFixture) { f.bodies[latestReleaseURL] = []byte(`{"tag_name":"../../ec"}`) }, "valid stable release"},
		{"prerelease tag", func(f *updateFixture) { f.bodies[latestReleaseURL] = []byte(`{"tag_name":"v0.3.0-rc.1"}`) }, "valid stable release"},
		{"prerelease", func(f *updateFixture) { f.bodies[latestReleaseURL] = []byte(`{"tag_name":"v0.3.0","prerelease":true}`) }, "valid stable release"},
		{"draft", func(f *updateFixture) { f.bodies[latestReleaseURL] = []byte(`{"tag_name":"v0.3.0","draft":true}`) }, "valid stable release"},
		{"missing asset", func(f *updateFixture) { f.bodies[latestReleaseURL] = []byte(`{"tag_name":"v0.2.0"}`) }, "is missing"},
		{"unsupported platform", func(f *updateFixture) { f.u.goarch = "386" }, "no release binary"},
		{"missing checksum asset", func(f *updateFixture) {
			f.bodies[latestReleaseURL] = fmt.Appendf(nil, `{"tag_name":"v0.2.0","assets":[{"name":%q}]}`, f.archiveName)
		}, "missing checksums.txt"},
		{"checksum download", func(f *updateFixture) { f.statuses[f.base+"checksums.txt"] = http.StatusNotFound }, "download checksums"},
		{"checksum missing", func(f *updateFixture) { f.bodies[f.base+"checksums.txt"] = []byte("nothing") }, "checksums.txt is missing"},
		{"checksum invalid", func(f *updateFixture) {
			f.bodies[f.base+"checksums.txt"] = fmt.Appendf(nil, "bogus  %s", f.archiveName)
		}, "invalid SHA-256"},
		{"checksum mismatch", func(f *updateFixture) { f.bodies[f.base+f.archiveName] = []byte("tampered") }, "checksum verification failed"},
		{"archive download", func(f *updateFixture) { f.statuses[f.base+f.archiveName] = http.StatusInternalServerError }, "download update"},
		{"archive invalid", func(f *updateFixture) { f.setArchive([]byte("not an archive")) }, "unpack update"},
		{"executable missing", func(f *updateFixture) {
			f.setArchive(makeUpdateArchive(t, "ec", []updateEntry{{name: "README.md", data: []byte("readme")}}))
		}, "archive is missing"},
		{"locate executable", func(f *updateFixture) {
			f.u.executable = func() (string, error) { return "", errors.New("no executable") }
		}, "locate ec executable"},
		{"install failure", func(f *updateFixture) { f.u.executable = func() (string, error) { return f.target + ".missing", nil } }, "install update"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := setupUpdate(t, "linux", []byte("new executable"))
			tt.change(f)
			var output bytes.Buffer
			err := f.u.run(t.Context(), "v0.1.0", &output)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if output.Len() != 0 {
				t.Errorf("unexpected success output: %q", output.String())
			}
			assertFile(t, f.target, "old executable")
		})
	}
}

func TestUpdateRejectsDevelopmentBuildWithoutRequests(t *testing.T) {
	for _, version := range []string{"dev", "", "(devel)", "unversioned"} {
		f := setupUpdate(t, "linux", []byte("new executable"))
		err := f.u.run(t.Context(), version, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "development build") || len(f.requests) != 0 {
			t.Fatalf("version %q: error = %v, requests = %v", version, err, f.requests)
		}
		assertFile(t, f.target, "old executable")
	}
}

func TestUpdateCancellationKeepsExecutable(t *testing.T) {
	f := setupUpdate(t, "linux", []byte("new executable"))
	ctx, cancel := context.WithCancel(t.Context())
	f.u.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		return nil, req.Context().Err()
	})
	if err := f.u.run(ctx, "v0.1.0", io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	assertFile(t, f.target, "old executable")
}

func TestUpdateChecksum(t *testing.T) {
	hash := sha256.Sum256([]byte("archive"))
	for _, checksums := range []string{
		fmt.Sprintf("%x  ec.tar.gz\n", hash),
		fmt.Sprintf("%x *ec.tar.gz\r\n", hash),
		fmt.Sprintf("%x  other.tar.gz\n%x  ec.tar.gz\n", hash, hash),
	} {
		got, err := updateChecksum([]byte(checksums), "ec.tar.gz")
		if err != nil || !bytes.Equal(got, hash[:]) {
			t.Fatalf("checksum = %x, %v", got, err)
		}
	}
	for _, checksums := range []string{
		fmt.Sprintf("%x  other.tar.gz", hash),
		"aa  ec.tar.gz", "zz  ec.tar.gz",
		fmt.Sprintf("%x  ec.tar.gz\n%x  ec.tar.gz", hash, hash),
	} {
		if _, err := updateChecksum([]byte(checksums), "ec.tar.gz"); err == nil {
			t.Fatalf("accepted invalid checksums: %q", checksums)
		}
	}
}

func TestUpdateDownloadSizeLimitAndReadFailure(t *testing.T) {
	f := setupUpdate(t, "linux", []byte("new executable"))
	f.bodies[latestReleaseURL] = []byte("12345")
	if _, err := f.u.download(t.Context(), latestReleaseURL, 4); err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("error = %v", err)
	}
	if _, err := f.u.download(t.Context(), latestReleaseURL, 5); err != nil {
		t.Fatal(err)
	}
	f.u.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: failingUpdateReader{}}, nil
	})
	if _, err := f.u.download(t.Context(), latestReleaseURL, 5); err == nil || !strings.Contains(err.Error(), "interrupted download") {
		t.Fatalf("error = %v", err)
	}
}

type failingUpdateReader struct{}

func (failingUpdateReader) Read([]byte) (int, error) { return 0, errors.New("interrupted download") }
func (failingUpdateReader) Close() error             { return nil }

func TestUpdateAssetNames(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			archive, binary, err := updateAssetNames("v1.2.3", goos, arch)
			if err != nil || !strings.HasPrefix(archive, "ec_1.2.3_"+goos+"_"+arch) {
				t.Fatalf("asset = %q, %q, %v", archive, binary, err)
			}
			if goos == "windows" && (binary != "ec.exe" || !strings.HasSuffix(archive, ".zip")) {
				t.Fatalf("Windows asset = %q, %q", archive, binary)
			}
			if goos != "windows" && (binary != "ec" || !strings.HasSuffix(archive, ".tar.gz")) {
				t.Fatalf("Unix asset = %q, %q", archive, binary)
			}
		}
	}
}

func TestUpdateMetadataAndChecksumSizeLimits(t *testing.T) {
	for _, endpoint := range []string{latestReleaseURL, updateRepository + "/releases/download/" + testUpdateTag + "/checksums.txt"} {
		f := setupUpdate(t, "linux", []byte("new executable"))
		f.bodies[endpoint] = bytes.Repeat([]byte("x"), maxUpdateMetadata+1)
		if err := f.u.run(t.Context(), "v0.1.0", io.Discard); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error = %v", err)
		}
		assertFile(t, f.target, "old executable")
	}
}

func TestUpdateReleaseIgnoresUntrustedAssetURLs(t *testing.T) {
	f := setupUpdate(t, "linux", []byte("new executable"))
	var release map[string]any
	if err := json.Unmarshal(f.bodies[latestReleaseURL], &release); err != nil {
		t.Fatal(err)
	}
	for _, asset := range release["assets"].([]any) {
		asset.(map[string]any)["browser_download_url"] = "http://untrusted.invalid/update"
	}
	f.bodies[latestReleaseURL], _ = json.Marshal(release)
	if err := f.u.run(t.Context(), "v0.1.0", io.Discard); err != nil {
		t.Fatal(err)
	}
	assertFile(t, f.target, "new executable")
}
