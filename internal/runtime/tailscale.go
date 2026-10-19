package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/ronigooja/Nagi/internal/i18n"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const TailscaleControlURL = "https://controlplane.tailscale.com"

func NormalizeTailscaleControlURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return TailscaleControlURL
	}
	return strings.TrimRight(value, "/")
}

// TailscaleStatePath never reads existing identity data. Every directory component
// is checked without following symbolic links before creating owner-only storage.
func TailscaleStatePath(root, profile, node, controlURL string) (string, error) {
	originalPath := TailscaleStateName(root, profile, node, controlURL)
	// Resolve only the fixed macOS system alias, never user-supplied links.
	if runtime.GOOS == "darwin" && strings.HasPrefix(root, "/var/") {
		root = "/private" + root
	}
	path := TailscaleStateName(root, profile, node, controlURL)

	if !filepath.IsAbs(path) {
		return "", i18n.Errorf("backend.runtime.tailscale_state_root_must_be_absolute", "Tailscale state root must be absolute")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", i18n.Errorf("backend.runtime.could_not_open_tailscale_state_root", "could not open Tailscale state root")
	}
	defer func() { _ = unix.Close(fd) }()
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e == unix.ENOENT {
			if e = unix.Mkdirat(fd, part, 0700); e != nil {
				return "", i18n.Errorf("backend.runtime.could_not_create_tailscale_state_directory", "could not create Tailscale state directory")
			}
			next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if e != nil {
			return "", i18n.Errorf("backend.runtime.unsafe_tailscale_state_directory", "unsafe Tailscale state directory")
		}
		_ = unix.Close(fd)
		fd = next
		if current == root || current == filepath.Join(root, ".tailscale-state") || current == path {
			var stat unix.Stat_t
			if unix.Fstat(fd, &stat) != nil || stat.Uid != uint32(os.Geteuid()) {
				return "", i18n.Errorf("backend.runtime.tailscale_state_directory_has_unexpected_ownership", "Tailscale state directory has unexpected ownership")
			}
			if unix.Fchmod(fd, 0700) != nil {
				return "", i18n.Errorf("backend.runtime.could_not_secure_tailscale_state_directory", "could not secure Tailscale state directory")
			}
		}
	}

	return originalPath, nil
}

func TailscaleStateName(root, profile, node, controlURL string) string {
	sum := sha256.Sum256([]byte(profile + "\x00" + node + "\x00" + NormalizeTailscaleControlURL(controlURL)))
	return filepath.Join(root, ".tailscale-state", hex.EncodeToString(sum[:]))
}

// TailscaleEnv grants only the managed identity subtree in addition to explicit
// caller safe paths, preserving mihomo's configuration-relative resource base.
func TailscaleEnv(env []string, root string) []string {
	result := make([]string, 0, len(env)+1)
	safe := ""
	for _, item := range env {
		if strings.HasPrefix(item, "SAFE_PATHS=") {
			safe = strings.TrimPrefix(item, "SAFE_PATHS=")
			continue
		}
		result = append(result, item)
	}
	managed := filepath.Join(root, ".tailscale-state")
	if safe != "" {
		safe += string(filepath.ListSeparator)
	}
	result = append(result, "SAFE_PATHS="+safe+managed)
	return result
}
