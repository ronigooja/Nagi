package server

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/profile"
	"gopkg.in/yaml.v3"
)

// Evidence is invalidated before forwarding a reload: a timeout cannot establish
// which source is active. No user request selects a root-readable filename.
type providerEvidence struct {
	profile, snapshot string
	hashes            map[string]string
}

func profileFromSource(source string) string {
	name := strings.TrimSuffix(filepath.Base(source), ".yaml")
	name = strings.TrimPrefix(name, ".effective-")
	return strings.TrimPrefix(name, ".restore-")
}
func (h *RuntimeHandler) bindProviderEvidence(name, source, snapshot string, data []byte) {
	h.providerEvidence = providerEvidence{}
	if name == "" || name != profileFromSource(source) {
		return
	}
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil {
		return
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	hashes := map[string]string{}
	for key, value := range providers {
		if source, ok := value.(map[string]any); ok {
			hashes[key] = profile.ProviderSourceHash(source)
		}
	}
	h.providerEvidence = providerEvidence{profile: name, snapshot: snapshot, hashes: hashes}
}
func (h *RuntimeHandler) providerChainNodes(req privileged.Request) (privileged.Response, error) {
	unavailable := errors.New("provider identity unavailable; inspect the active profile and refresh its provider")
	evidence := h.providerEvidence
	if evidence.profile != req.Profile || evidence.snapshot == "" || evidence.hashes[req.Provider] != req.SourceHash {
		return privileged.Response{}, unavailable
	}
	data, err := readUserConfig(h.PrivateDir, evidence.snapshot, 0)
	if err != nil {
		return privileged.Response{}, unavailable
	}
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil {
		return privileged.Response{}, unavailable
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	source, _ := providers[req.Provider].(map[string]any)
	if source == nil {
		return privileged.Response{}, unavailable
	}
	kind, _ := source["type"].(string)
	var values []any
	if kind == "inline" {
		values, _ = source["payload"].([]any)
	} else {
		path, _ := source["path"].(string)
		if path == "" && kind == "http" {
			url, _ := source["url"].(string)
			path = filepath.Join("proxies", fmt.Sprintf("%x", md5.Sum([]byte(url))))
		}
		if path == "" {
			return privileged.Response{}, unavailable
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(h.PrivateDir, path)
		}
		data, err = readUserConfig(h.PrivateDir, path, 0)
		if err != nil {
			return privileged.Response{}, unavailable
		}
		var payload map[string]any
		if yaml.Unmarshal(data, &payload) != nil {
			return privileged.Response{}, unavailable
		}
		values, _ = payload["proxies"].([]any)
	}
	// Project immediately; no server, password, key, URL or other node field is
	// ever serialized across the privilege boundary.
	nodes := []map[string]string{}
	for _, value := range values {
		node, _ := value.(map[string]any)
		name, _ := node["name"].(string)
		kind, _ := node["type"].(string)
		if name == "" || len(name) > 4096 || len(kind) > 128 {
			continue
		}
		nodes = append(nodes, map[string]string{"name": name, "type": kind})
	}
	if len(nodes) == 0 {
		return privileged.Response{}, unavailable
	}
	body, err := json.Marshal(nodes)
	if err != nil || len(body) > privileged.MaxMessageBytes-1024 {
		return privileged.Response{}, unavailable
	}
	return privileged.Response{OK: true, Body: body}, nil
}
