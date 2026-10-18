package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

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
