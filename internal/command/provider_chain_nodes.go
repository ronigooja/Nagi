package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

// Running privileged providers live in the helper's private working directory.
// Ask only for authenticated identity evidence; the helper retains all payloads.
func configureProviderChainNodes(ctx context.Context, store *profile.Store, paths nagiruntime.Paths, client *control.Client) {
	mode, err := readBackend(paths)
	if err != nil || mode != privilegedBackend {
		return
	}
	b := helperBackend{paths: paths}
	store.ProviderNodes = providerChainNodeResolver(ctx, b, client)
}

func providerChainNodeResolver(ctx context.Context, b helperBackend, client *control.Client) func(string, string, map[string]any) ([]map[string]any, error) {
	return func(name, provider string, source map[string]any) ([]map[string]any, error) {
		status, err := b.Status(ctx)
		if err != nil {
			return nil, err
		}
		if !status.Running {
			return nil, nil
		} // stopped inspection uses local evidence
		response, err := b.call(ctx, privileged.Request{Operation: privileged.ProviderChainNodes, Profile: name, Provider: provider, SourceHash: profile.ProviderSourceHash(source)})
		if err != nil {
			return nil, err
		}
		var nodes []map[string]any
		if json.Unmarshal(response.Body, &nodes) != nil {
			return nil, fmt.Errorf("provider identity unavailable")
		}
		var live struct {
			Proxies []struct {
				Name string `json:"name"`
			} `json:"proxies"`
		}
		if err := client.Get(ctx, "/providers/proxies/"+url.PathEscape(provider), &live); err != nil {
			return nil, err
		}
		// Raw evidence preserves payload order. Store applies source filters/renames;
		// only candidates still exposed by the running provider may be offered.
		available := map[string]bool{}
		for _, node := range live.Proxies {
			available[node.Name] = true
		}
		return profile.FilterProviderChainEvidence(source, nodes, available)
	}
}
