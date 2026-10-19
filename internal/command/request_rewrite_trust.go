package command

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"

	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"golang.org/x/sys/unix"
)

var rewriteTrustAssessment = assessRewriteTrust
var rewriteTrustCommand = func(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "/usr/bin/security", args...).Run()
}

func requestRewriteCAStatus(paths nagiruntime.Paths) (rewrite.CAStatus, error) {
	store := rewrite.CAStore{ConfigDir: paths.ConfigDir}
	status, e := store.Status(rewriteNow())
	if e != nil || status.State != "generated" {
		return status, e
	}
	public, e := store.ExportPublic(rewriteNow())
	if e != nil {
		return status, e
	}
	status.Trust = rewriteTrustAssessment(public)
	return status, nil
}
func assessRewriteTrust(public []byte) string {
	if runtime.GOOS == "darwin" {
		f, e := os.CreateTemp("", "nagi-public-ca-*")
		if e != nil {
			return "unknown"
		}
		defer os.Remove(f.Name())
		if _, e = f.Write(public); e != nil {
			f.Close()
			return "unknown"
		}
		if f.Close() != nil {
			return "unknown"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if rewriteTrustCommand(ctx, "verify-cert", "-q", "-L", "-c", f.Name(), "-p", "basic") == nil {
			return "trusted"
		}
		return "untrusted"
	}
	block, _ := pem.Decode(public)
	if block == nil {
		return "unknown"
	}
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		return "unknown"
	}
	roots, e := rewriteFreshSystemRoots()
	if e != nil {
		return "unknown"
	}
	_, e = cert.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	if e == nil {
		return "trusted"
	}
	return "untrusted"
}

// OS trust mutation is reached only by an explicit public trust command. Unit
// tests replace rewriteTrustCommand and never run a real keychain operation.
func requestRewriteCATrust(ctx context.Context, paths nagiruntime.Paths, install bool) (rewrite.CAStatus, error) {
	if !install {
		if e := rewriteTeardown(ctx, paths); e != nil {
			return rewrite.CAStatus{}, e
		}
	}
	store := rewrite.CAStore{ConfigDir: paths.ConfigDir}
	public, e := store.ExportPublic(rewriteNow())
	if e != nil {
		return rewrite.CAStatus{}, rewriteDomainError(e)
	}
	if runtime.GOOS != "darwin" {
		status, _ := requestRewriteCAStatus(paths)
		return status, fail("request_rewrite_error", messageError("cli.error.request_rewrite_trust.rewrite_trust_manual_export_the_public_certificate_with_nagi_request", "rewrite_trust_manual: export the public certificate with `nagi request-rewrite ca export FILE`, then use your distribution or application certificate trust settings; removal must use the same store; run `nagi request-rewrite ca status` afterwards"))
	}
	if os.Geteuid() == 0 {
		return rewrite.CAStatus{}, rewriteFailure("rewrite_auth")
	}
	block, _ := pem.Decode(public)
	if block == nil {
		return rewrite.CAStatus{}, rewriteFailure("rewrite_ca_invalid")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return rewrite.CAStatus{}, rewriteFailure("rewrite_ca_trust_unknown")
	}
	keychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
	if install {
		if e = paths.EnsureRuntime(); e != nil {
			return rewrite.CAStatus{}, rewriteFailure("rewrite_storage")
		}
		f, e := os.CreateTemp(paths.RuntimeDir, ".rewrite-public-ca-*")
		if e != nil {
			return rewrite.CAStatus{}, rewriteFailure("rewrite_storage")
		}
		path := f.Name()
		defer os.Remove(path)
		if e = f.Chmod(0600); e == nil {
			_, e = f.Write(public)
		}
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			return rewrite.CAStatus{}, rewriteFailure("rewrite_storage")
		}
		e = rewriteTrustCommand(ctx, "add-trusted-cert", "-r", "trustRoot", "-k", keychain, path)
	} else {
		sum := sha1.Sum(block.Bytes)
		e = rewriteTrustCommand(ctx, "delete-certificate", "-t", "-Z", hex.EncodeToString(sum[:]), keychain)
	}
	if e != nil {
		return rewrite.CAStatus{}, rewriteFailure("rewrite_ca_trust_failed")
	}
	status, e := requestRewriteCAStatus(paths)
	if e != nil {
		return status, rewriteDomainError(e)
	}
	expected := "untrusted"
	if install {
		expected = "trusted"
	}
	if status.Trust != expected {
		return status, rewriteFailure("rewrite_ca_trust_unknown")
	}
	return status, nil
}

// Linux roots are reread for each assessment. SystemCertPool caches its first
// snapshot processwide, which is unsuitable for a persistent supervisor.
func rewriteFreshSystemRoots() (*x509.CertPool, error) {
	roots := x509.NewCertPool()
	files := []string{"/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt", "/etc/ssl/ca-bundle.pem", "/etc/pki/tls/cacert.pem", "/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", "/etc/ssl/cert.pem"}
	dirs := []string{"/etc/ssl/certs", "/etc/pki/tls/certs"}
	if value := os.Getenv("SSL_CERT_FILE"); value != "" {
		files = []string{value}
	}
	if value := os.Getenv("SSL_CERT_DIR"); value != "" {
		dirs = strings.Split(value, ":")
	}
	total := int64(0)
	loaded := false
	read := func(path string) bool {
		f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
		if e != nil {
			return false
		}
		defer f.Close()
		if info, e := f.Stat(); e != nil || !info.Mode().IsRegular() {
			return false
		}
		b, e := io.ReadAll(io.LimitReader(f, 4<<20+1))
		if e != nil || len(b) > 4<<20 {
			return false
		}
		total += int64(len(b))
		if total > 16<<20 {
			return false
		}
		return roots.AppendCertsFromPEM(b)
	}
	for _, file := range files {
		if read(file) {
			loaded = true
			break
		}
	}
	for _, dir := range dirs {
		entries, e := os.ReadDir(dir)
		if e != nil {
			continue
		}
		if len(entries) > 4096 {
			return nil, messageError("cli.error.request_rewrite_trust.certificate_store_limit", "certificate store limit")
		}
		for _, entry := range entries {
			if !entry.IsDir() && read(filepath.Join(dir, entry.Name())) {
				loaded = true
			}
			if total > 16<<20 {
				return nil, messageError("cli.error.request_rewrite_trust.certificate_store_limit", "certificate store limit")
			}
		}
	}
	if !loaded {
		return nil, messageError("cli.error.request_rewrite_trust.certificate_store_unavailable", "certificate store unavailable")
	}
	return roots, nil
}
