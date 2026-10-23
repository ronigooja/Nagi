package installation

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ronigooja/Nagi/internal/i18n"
)

var releaseTag = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)

func ValidTag(tag string) bool { return releaseTag.MatchString(tag) }

const archiveLimit = 512 << 20

const releaseConnectionTimeout = 10 * time.Second

const releaseMinimumSpeed = 100 * 1024 // bytes per second

// Watch a rolling ten-sample window, including reads blocked by a stalled
// server. Closing the HTTP response body interrupts any pending Read.
type speedCheckedBody struct {
	io.ReadCloser
	bytes   atomic.Int64
	slow    atomic.Bool
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func watchReleaseBody(body io.ReadCloser, minimum int64, interval time.Duration) *speedCheckedBody {
	b := &speedCheckedBody{ReadCloser: body, done: make(chan struct{}), stopped: make(chan struct{})}
	go func() {
		defer close(b.stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var history [10]int64
		var times [10]time.Time
		start := time.Now()
		for i := range times {
			times[i] = start
		}
		ticks := 0
		for {
			select {
			case <-b.done:
				return
			case now := <-ticker.C:
				index := ticks % len(history)
				current := b.bytes.Load()
				ticks++
				if ticks >= len(history) && float64(current-history[index]) < float64(minimum)*now.Sub(times[index]).Seconds() {
					b.slow.Store(true)
					body.Close()
					return
				}
				history[index], times[index] = current, now
			}
		}
	}()
	return b
}

func (b *speedCheckedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytes.Add(int64(n))
	if b.slow.Load() {
		return n, i18n.Errorf("system.installation.download.too.slow", "download stayed below 100 KiB/s for 10 seconds")
	}
	return n, err
}

func (b *speedCheckedBody) Close() error {
	var err error
	b.once.Do(func() {
		close(b.done)
		err = b.ReadCloser.Close()
		<-b.stopped
	})
	return err
}

func releaseHTTPClient(client *http.Client) *http.Client {
	var copy http.Client
	if client != nil {
		copy = *client
	}
	// A responsive download has no whole-request deadline. Bound connection
	// establishment and response headers independently of archive transfer time.
	copy.Timeout = 0
	transport := copy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if original, ok := transport.(*http.Transport); ok {
		bounded := original.Clone()
		bounded.DialContext = (&net.Dialer{Timeout: releaseConnectionTimeout, KeepAlive: 30 * time.Second}).DialContext
		bounded.TLSHandshakeTimeout = releaseConnectionTimeout
		bounded.ResponseHeaderTimeout = releaseConnectionTimeout
		copy.Transport = bounded
	}
	return &copy
}

// Fetch verifies the release checksum before extracting anything executable.
func Fetch(ctx context.Context, base, target, tag string) (string, func(), error) {
	return fetchWithClient(ctx, base, target, tag, nil)
}

func fetchWithClient(ctx context.Context, base, target, tag string, client *http.Client) (string, func(), error) {
	return fetchWithProgress(ctx, base, target, tag, client, nil)
}

func fetchWithProgress(ctx context.Context, base, target, tag string, client *http.Client, progress func(*i18n.Message)) (string, func(), error) {
	client = releaseHTTPClient(client)
	defer client.CloseIdleConnections()
	artifact, cleanup, err := fetchFromSource(ctx, base, target, tag, client, progress)
	var downloadErr *releaseDownloadError
	if err != nil && ctx.Err() == nil && strings.HasPrefix(base, "https://github.com/") &&
		strings.HasSuffix(base, "/releases") && errors.As(err, &downloadErr) {
		if progress != nil {
			progress(i18n.Errorf("system.installation.progress.fallback", "GitHub download failed: %s; switching to gh-proxy.com...", err))
		}
		return fetchFromSource(ctx, "https://gh-proxy.com/"+base, target, tag, client, progress)
	}
	return artifact, cleanup, err
}

// Only download failures can switch sources. Invalid content and local failures
// remain errors, including a checksum mismatch from either source.
type releaseDownloadError struct{ error }

func (e *releaseDownloadError) Unwrap() error { return e.error }

func fetchFromSource(ctx context.Context, base, target, tag string, client *http.Client, progress func(*i18n.Message)) (string, func(), error) {
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
	asset := "nagi-" + target + ".tar.gz"
	if progress != nil {
		source := "GitHub"
		if location, err := url.Parse(base); err == nil && location.Hostname() != "" {
			source = location.Hostname()
		}
		progress(i18n.Errorf("system.installation.progress.checksums", "Downloading checksums from %s...", source))
	}
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
	if progress != nil {
		progress(i18n.Errorf("system.installation.progress.archive", "Downloading %s...", asset))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+asset, nil)
	if err != nil {
		return "", nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", nil, &releaseDownloadError{i18n.Errorf("system.installation.download", "download %s: %w", asset, err)}
	}
	response.Body = watchReleaseBody(response.Body, releaseMinimumSpeed, time.Second)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, &releaseDownloadError{i18n.Errorf("system.installation.download.http", "download %s: HTTP %d", asset, response.StatusCode)}
	}
	archive, err := os.Create(filepath.Join(dir, asset))
	if err != nil {
		return "", nil, err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(response.Body, archiveLimit+1))
	closeErr := archive.Close()
	if copyErr != nil {
		var localErr *os.PathError
		if errors.As(copyErr, &localErr) {
			return "", nil, copyErr
		}
		return "", nil, &releaseDownloadError{i18n.Errorf("system.installation.download", "download %s: %w", asset, copyErr)}
	}
	if closeErr != nil {
		return "", nil, closeErr
	}
	if n > archiveLimit {
		return "", nil, i18n.Errorf("system.installation.release.archive.exceeds.512.mib", "release archive exceeds 512 MiB")
	}
	if progress != nil {
		progress(i18n.Errorf("system.installation.progress.checksum", "Verifying archive checksum..."))
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return "", nil, i18n.Errorf("system.installation.release.archive.checksum.mismatch", "release archive checksum mismatch")
	}
	artifact := filepath.Join(dir, "artifact")
	if progress != nil {
		progress(i18n.Errorf("system.installation.progress.extract", "Extracting release archive..."))
	}
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
		return nil, &releaseDownloadError{i18n.Errorf("system.installation.download", "download %s: %w", "SHA256SUMS", err)}
	}
	response.Body = watchReleaseBody(response.Body, releaseMinimumSpeed, time.Second)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &releaseDownloadError{i18n.Errorf("system.installation.release.checksum.download.http", "release checksum download: HTTP %d", response.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, &releaseDownloadError{i18n.Errorf("system.installation.download", "download %s: %w", "SHA256SUMS", err)}
	}
	if int64(len(data)) > limit {
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
