package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const testUpdateTag = "v0.2.0"

type updateFixture struct {
	u           updater
	target      string
	archiveName string
	base        string
	bodies      map[string][]byte
	statuses    map[string]int
	requests    []string
}

func setupUpdate(t *testing.T, goos string, binary []byte) *updateFixture {
	t.Helper()
	archiveName, binaryName, err := updateAssetNames(testUpdateTag, goos, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	f := &updateFixture{
		target:      filepath.Join(t.TempDir(), "ec"),
		archiveName: archiveName,
		base:        updateRepository + "/releases/download/" + testUpdateTag + "/",
		bodies:      make(map[string][]byte),
		statuses:    make(map[string]int),
	}
	if err := os.WriteFile(f.target, []byte("old executable"), 0o751); err != nil {
		t.Fatal(err)
	}
	f.u = updater{
		executable: func() (string, error) { return f.target, nil },
		goos:       goos, goarch: "arm64",
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
				t.Errorf("unexpected update request method or credentials: %v", req)
			}
			if req.Header.Get("User-Agent") == "" {
				t.Error("missing User-Agent")
			}
			endpoint := req.URL.String()
			f.requests = append(f.requests, endpoint)
			data, ok := f.bodies[endpoint]
			if !ok {
				return nil, fmt.Errorf("unexpected request: %s", endpoint)
			}
			status := f.statuses[endpoint]
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(data)),
			}, nil
		})},
	}
	f.bodies[latestReleaseURL] = []byte(fmt.Sprintf(`{"tag_name":%q,"assets":[{"name":%q},{"name":"checksums.txt"}]}`, testUpdateTag, archiveName))
	f.setArchive(makeUpdateArchive(t, binaryName, []updateEntry{{name: binaryName, data: binary}}))
	return f
}

func (f *updateFixture) setArchive(archive []byte) {
	f.bodies[f.base+f.archiveName] = archive
	f.bodies[f.base+"checksums.txt"] = fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(archive), f.archiveName)
}

type updateEntry struct {
	name    string
	data    []byte
	symlink bool
}

func makeUpdateArchive(t *testing.T, binaryName string, entries []updateEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	if binaryName == "ec.exe" {
		zw := zip.NewWriter(&output)
		for _, entry := range entries {
			header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
			header.SetMode(0o755)
			if entry.symlink {
				header.SetMode(os.ModeSymlink | 0o755)
			}
			w, err := zw.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&output)
		tw := tar.NewWriter(gz)
		for _, entry := range entries {
			header := &tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(entry.data)), Typeflag: tar.TypeReg}
			if entry.symlink {
				header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, "elsewhere", 0
			}
			if err := tw.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if !entry.symlink {
				if _, err := tw.Write(entry.data); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return output.Bytes()
}

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
			f.u.executable = func() (string, error) {
				t.Fatal("up-to-date check tried to locate executable")
				return "", nil
			}
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

func TestUpdateCommandRejectsFlagsAndArguments(t *testing.T) {
	for _, args := range [][]string{{"update", "--check"}, {"update", "v0.2.0"}, {"update", "--force"}} {
		if err := Run(t.Context(), args, io.Discard, "v0.1.0"); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	cmd := newUpdateCommand("dev")
	cmd.SetArgs([]string{})
	cmd.SetOut(io.Discard)
	if err := cmd.ExecuteContext(t.Context()); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("command did not run updater: %v", err)
	}
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

func TestUnpackUpdate(t *testing.T) {
	for _, name := range []string{"ec", "ec.exe"} {
		t.Run(name, func(t *testing.T) {
			archive := makeUpdateArchive(t, name, []updateEntry{
				{name: "README.md", data: []byte("readme")},
				{name: "../outside", data: []byte("ignored")},
				{name: name, data: []byte("executable")},
			})
			got, err := unpackUpdate(archive, name)
			if err != nil || string(got) != "executable" {
				t.Fatalf("unpack = %q, %v", got, err)
			}
			for _, entries := range [][]updateEntry{
				{{name: name}},
				{{name: "../" + name, data: []byte("traversal")}},
				{{name: name, symlink: true}},
				{{name: name, data: []byte("one")}, {name: name, data: []byte("two")}},
			} {
				if _, err := unpackUpdate(makeUpdateArchive(t, name, entries), name); err == nil {
					t.Fatalf("accepted invalid entries: %v", entries)
				}
			}
			if _, err := unpackUpdate([]byte("bad archive"), name); err == nil {
				t.Fatal("accepted invalid archive")
			}
			if _, err := unpackUpdate(archive[:len(archive)/2], name); err == nil {
				t.Fatal("accepted truncated archive")
			}
		})
	}
}

func TestUnpackUpdateRejectsGzipChecksumFailure(t *testing.T) {
	archive := makeUpdateArchive(t, "ec", []updateEntry{{name: "ec", data: []byte("executable")}})
	archive[len(archive)-8] ^= 0xff
	if _, err := unpackUpdate(archive, "ec"); !errors.Is(err, gzip.ErrChecksum) {
		t.Fatalf("error = %v, want gzip checksum error", err)
	}
}

func TestUnpackUpdateRejectsOversizedExecutable(t *testing.T) {
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "ec", Mode: 0o755, Typeflag: tar.TypeReg, Size: maxUpdateBinary + 1}); err != nil {
		t.Fatal(err)
	}
	// Deliberately omit the payload: its declared size must be rejected first.
	_ = tw.Close()
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := unpackUpdate(output.Bytes(), "ec"); err == nil || !strings.Contains(err.Error(), "invalid or duplicate") {
		t.Fatalf("oversized tar executable: %v", err)
	}
	archive := makeUpdateArchive(t, "ec.exe", []updateEntry{{name: "ec.exe", data: []byte("executable")}})
	central := bytes.Index(archive, []byte("PK\x01\x02"))
	if central < 0 {
		t.Fatal("missing zip central directory")
	}
	binary.LittleEndian.PutUint32(archive[central+24:central+28], maxUpdateBinary+1)
	if _, err := unpackUpdate(archive, "ec.exe"); err == nil || !strings.Contains(err.Error(), "invalid or duplicate") {
		t.Fatalf("oversized zip executable: %v", err)
	}
}

func TestUnpackUpdateBoundsSkippedTarEntries(t *testing.T) {
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "ec", Mode: 0o755, Typeflag: tar.TypeReg, Size: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("ec")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	// Extra decompressed data after the tar end marker must also stay bounded.
	if _, err := io.CopyN(gz, zeroUpdateReader{}, maxUnpackedUpdate); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := unpackUpdate(output.Bytes(), "ec"); err == nil || !strings.Contains(err.Error(), "unpacked update exceeds") {
		t.Fatalf("oversized decompressed archive: %v", err)
	}
}

type zeroUpdateReader struct{}

func (zeroUpdateReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
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

func TestReplaceExecutableThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires elevated privileges")
	}
	f := setupUpdate(t, "linux", []byte("new executable"))
	link := filepath.Join(t.TempDir(), "ec-link")
	if err := os.Symlink(f.target, link); err != nil {
		t.Fatal(err)
	}
	f.u.executable = func() (string, error) { return link, nil }
	if err := f.u.run(t.Context(), "v0.1.0", io.Discard); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was replaced: %v, %v", info, err)
	}
	assertFile(t, f.target, "new executable")
	assertFile(t, link, "new executable")
}

func TestCommitUpdateFailurePreservesExecutable(t *testing.T) {
	f := setupUpdate(t, "linux", []byte("new executable"))
	if err := commitUpdate(filepath.Join(t.TempDir(), "missing"), f.target); err == nil {
		t.Fatal("expected rename failure")
	}
	assertFile(t, f.target, "old executable")
}

func TestReplaceExecutableRejectsDirectory(t *testing.T) {
	if err := replaceExecutable(t.TempDir(), []byte("new executable")); err == nil || !strings.Contains(err.Error(), "not a regular executable") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateReadOnlyInstallationKeepsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions without root privileges")
	}
	f := setupUpdate(t, "linux", []byte("new executable"))
	dir := filepath.Dir(f.target)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := f.u.run(t.Context(), "v0.1.0", io.Discard); err == nil || !strings.Contains(err.Error(), "installation directory") {
		t.Fatalf("error = %v", err)
	}
	assertFile(t, f.target, "old executable")
}

// Exercise replacement of a running process on each native CI platform.
func TestUpdateRunningExecutable(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "ec-test")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := os.WriteFile(target, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	// Stat an open handle to capture the original Windows file ID before replacement.
	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.Stat()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), target, "-test.run=^TestUpdateRunningExecutableHelper$")
	cmd.Env = append(os.Environ(), "EC_UPDATE_TEST_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("Updated ec from v0.1.0 to v0.2.0")) {
		t.Fatalf("running update: %v\n%s", err, output)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("executable file was not replaced")
	}
	cmd = exec.CommandContext(t.Context(), target, "-test.run=^TestUpdateRunningExecutableHelper$")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("updated executable does not run: %v\n%s", err, output)
	}
}

func TestUpdateRunningExecutableHelper(t *testing.T) {
	if os.Getenv("EC_UPDATE_TEST_HELPER") != "1" {
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	f := setupUpdate(t, runtime.GOOS, binary)
	f.u.executable = os.Executable
	if err := f.u.run(t.Context(), "v0.1.0", os.Stdout); err != nil {
		t.Fatal(err)
	}
}

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
