package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}
