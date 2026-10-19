package installation

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/i18n"
)

var releaseTag = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)

func ValidTag(tag string) bool { return releaseTag.MatchString(tag) }

const archiveLimit = 512 << 20

// Fetch verifies the release checksum before extracting anything executable.
func Fetch(ctx context.Context, base, target, tag string) (string, func(), error) {
	return fetchWithClient(ctx, base, target, tag, nil)
}

func fetchWithClient(ctx context.Context, base, target, tag string, client *http.Client) (string, func(), error) {
	if tag != "" && !ValidTag(tag) {
		return "", nil, i18n.Errorf("system.installation.invalid.release.tag.use.vx.y.z", "invalid release tag; use vX.Y.Z")
	}
	if tag == "" {
		base += "/latest/download"
	} else {
		base += "/download/v" + strings.TrimPrefix(tag, "v")
	}
	dir, err := os.MkdirTemp("", "nagi-release-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	ok := false
	defer func() {
		if !ok {
			cleanup()
		}
	}()
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	asset := "nagi-" + target + ".tar.gz"
	sums, err := get(ctx, client, base+"/SHA256SUMS", 1<<20)
	if err != nil {
		return "", nil, err
	}
	expected := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if digest, err := hex.DecodeString(expected); err != nil || len(digest) != sha256.Size {
		return "", nil, i18n.Errorf("system.installation.sha256sums.has.no.valid.entry.for", "SHA256SUMS has no valid entry for %s", asset)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+asset, nil)
	if err != nil {
		return "", nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", nil, i18n.Errorf("system.installation.download", "download %s: %w", asset, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, i18n.Errorf("system.installation.download.http", "download %s: HTTP %d", asset, response.StatusCode)
	}
	archive, err := os.Create(filepath.Join(dir, asset))
	if err != nil {
		return "", nil, err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(response.Body, archiveLimit+1))
	closeErr := archive.Close()
	if copyErr != nil {
		return "", nil, copyErr
	}
	if closeErr != nil {
		return "", nil, closeErr
	}
	if n > archiveLimit {
		return "", nil, i18n.Errorf("system.installation.release.archive.exceeds.512.mib", "release archive exceeds 512 MiB")
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return "", nil, i18n.Errorf("system.installation.release.archive.checksum.mismatch", "release archive checksum mismatch")
	}
	artifact := filepath.Join(dir, "artifact")
	if err := extract(archive.Name(), artifact); err != nil {
		return "", nil, err
	}
	for _, name := range []string{"nagi", "mihomo", "installer.sh"} {
		info, err := os.Lstat(filepath.Join(artifact, name))
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 || info.Size() == 0 {
			return "", nil, i18n.Errorf("system.installation.release.has.no.regular.executable", "release has no regular executable %s", name)
		}
	}
	ok = true
	return artifact, cleanup, nil
}

func get(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, i18n.Errorf("system.installation.release.checksum.download.http", "release checksum download: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = i18n.Errorf("system.installation.checksum.file.is.too.large", "checksum file is too large")
	}
	return data, err
}

func extract(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer z.Close()
	reader := tar.NewReader(z)
	var total int64
	entries := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(header.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\r\n") {
			return i18n.Errorf("system.installation.unsafe.release.archive.path", "unsafe release archive path")
		}
		if name == "." {
			continue
		}
		entries++
		if entries > 100000 {
			return i18n.Errorf("system.installation.release.archive.has.too.many.entries", "release archive has too many entries")
		}
		path := filepath.Join(dest, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if header.Size < 0 || total > archiveLimit {
				return i18n.Errorf("system.installation.extracted.release.exceeds.512.mib", "extracted release exceeds 512 MiB")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode)&0777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return i18n.Errorf("system.installation.release.archive.contains.unsupported.links.or.special.files", "release archive contains unsupported links or special files")
		}
	}
}
