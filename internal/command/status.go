package command

import (
	"context"
	"sync"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/traffic"
)

// appendStatusReads runs independent enrichment only after lifecycle status and
// any stopped-engine recovery have completed. Each read owns its result; the
// response map is updated by the caller's goroutine after all reads finish.
func appendStatusReads(ctx context.Context, data map[string]any, running bool, client *control.Client, dnsStatus func(context.Context) (traffic.DNSState, error)) map[string]i18n.Descriptor {
	messages := map[string]i18n.Descriptor{}
	var wg sync.WaitGroup
	var dns traffic.DNSState
	var dnsErr, versionErr, configErr error
	var version struct {
		Version string `json:"version"`
	}
	var config map[string]any
	if dnsStatus != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dns, dnsErr = dnsStatus(ctx)
		}()
	}
	if running {
		wg.Add(2)
		go func() {
			defer wg.Done()
			versionErr = client.Get(ctx, "/version", &version)
		}()
		go func() {
			defer wg.Done()
			configErr = client.Get(ctx, "/configs", &config)
		}()
	}
	wg.Wait()
	if dnsStatus != nil {
		data["system_dns"] = dns
		if dnsErr != nil {
			data["system_dns_error"] = output.RedactError(dnsErr.Error())
			rememberError(messages, "/system_dns_error", dnsErr)
		}
	}
	if running && versionErr == nil {
		data["version"] = version.Version
	}
	if running && configErr == nil {
		data["mixed_port"] = config["mixed-port"]
	}
	return messages
}
