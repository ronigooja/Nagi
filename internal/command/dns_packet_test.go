package command

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDNSPacketRunner struct {
	interfaces []string
	err        error
	capture    string
	attempted  bool
}

func (f fakeDNSPacketRunner) Interfaces() ([]string, error)                   { return f.interfaces, f.err }
func (f fakeDNSPacketRunner) RouteTo(context.Context, string) (string, error) { return "en0", nil }
func (f fakeDNSPacketRunner) CaptureAndResolve(_ context.Context, interfaces []string, domain string) (map[string]string, bool, error) {
	return map[string]string{interfaces[0]: strings.ReplaceAll(f.capture, "PROBE", domain)}, f.attempted, nil
}

func TestPacketSampleClassifiesOnlyMatchingQuery(t *testing.T) {
	obs := systemDNSObservation{Resolvers: []string{"192.0.2.53"}}
	runner := fakeDNSPacketRunner{interfaces: []string{"en0"}, attempted: true, capture: "IP 192.0.2.2.12345 > 192.0.2.53.53: 1+ A? PROBE. (32)\n"}
	result := packetSample(context.Background(), obs, runner)
	if result.Status != "plaintext_seen" || len(result.MatchedInterfaces) != 1 || result.ResolverRoutes["192.0.2.53"] != "en0" {
		t.Fatalf("sample = %+v", result)
	}
	runner.capture = "IP 192.0.2.2.12345 > 192.0.2.53.53: 1+ A? other.example. (32)\n"
	result = packetSample(context.Background(), obs, runner)
	if result.Status != "no_plaintext_seen" || len(result.MatchedInterfaces) != 0 {
		t.Fatalf("unrelated query matched: %+v", result)
	}
	runner.attempted = false
	result = packetSample(context.Background(), obs, runner)
	if result.Status != "unavailable" {
		t.Fatalf("missing query reported: %+v", result)
	}
	runner.err = errors.New("tcpdump unavailable")
	result = packetSample(context.Background(), obs, runner)
	if result.Status != "unavailable" {
		t.Fatalf("preflight error reported: %+v", result)
	}
}

func TestPacketSampleInvocationSyntax(t *testing.T) {
	if err := validateInvocation([]string{"dns", "check", "--packet-sample"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"dns", "check", "--unknown"}, {"dns", "status", "--packet-sample"}, {"dns", "check", "--packet-sample", "extra"}} {
		if err := validateInvocation(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
