package configpolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"gopkg.in/yaml.v3"
)

var generatedChainProvider = regexp.MustCompile(`\Anagi-chain-[a-f0-9]{24}-source\z`)
var generatedChainCache = regexp.MustCompile(`\A[a-f0-9]{32}\.yaml\z`)

// SnapshotWithProviderCaches accepts only helper-created cache files within the
// root-private cache directory. Ordinary Validate still rejects HTTP paths.
// The helper never forwards or opens a caller's HTTP cache path as root.
func SnapshotWithProviderCaches(rootDir string, data []byte) (string, error) {
	if err := securePrivateDirectory(rootDir); err != nil {
		return "", err
	}
	var cfg yaml.Node
	if err := yaml.Unmarshal(data, &cfg); err != nil || len(cfg.Content) != 1 {
		return "", fmt.Errorf("invalid configuration")
	}
	root := cfg.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "proxy-providers" {
			continue
		}
		providers := root.Content[i+1]
		for j := 0; j+1 < len(providers.Content); j += 2 {
			name := providers.Content[j].Value
			p := providers.Content[j+1]
			fields, err := mapping(p)
			if err != nil {
				return "", err
			}
			kind := fields["type"]
			path := fields["path"]
			if kind == nil || kind.Value != "http" || path == nil {
				continue
			}
			cacheDir := filepath.Join(rootDir, ".chain-cache")
			if !generatedChainProvider.MatchString(name) || filepath.Dir(path.Value) != cacheDir || !generatedChainCache.MatchString(filepath.Base(path.Value)) {
				return "", fmt.Errorf("unsafe generated provider cache")
			}
			if err := securePrivateDirectory(cacheDir); err != nil {
				return "", err
			}
			info, err := os.Lstat(path.Value)
			if err != nil {
				return "", err
			}
			owner, ok := info.Sys().(*syscall.Stat_t)
			if !ok || owner.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
				return "", fmt.Errorf("unsafe generated provider cache file")
			}
			for k := 0; k+1 < len(p.Content); k += 2 {
				if p.Content[k].Value == "path" {
					p.Content = append(p.Content[:k], p.Content[k+2:]...)
					break
				}
			}
		}
	}
	sanitized, err := yaml.Marshal(&cfg)
	if err != nil {
		return "", err
	}
	if err = Validate(sanitized); err != nil {
		return "", err
	}
	// Bytes have passed ordinary policy with the exact controlled exceptions
	// checked above. SnapshotResource performs the same private atomic write.
	return SnapshotResource(rootDir, data)
}
