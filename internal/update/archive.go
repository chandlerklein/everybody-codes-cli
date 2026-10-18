package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

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
