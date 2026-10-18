package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
