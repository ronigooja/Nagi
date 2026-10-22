package command

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/profile"
)

func TestSubscriptionCompositionFailureIsNotRuleConflict(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	source := []byte("proxies:\n  - {name: hop, type: socks5, server: hop.example, port: 1080}\n  - {name: app, type: socks5, server: app.example, port: 1080}\nrules: [MATCH,DIRECT]\n")
	if err := s.Write(context.Background(), "work", source); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateProxyChain(context.Background(), "work", "subscription", []string{"work"}, []string{"hop"}, false); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			before[path] = string(data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A native group member disappeared in the candidate. No custom rules exist.
	candidate := append(append([]byte{}, source...), []byte("proxy-groups:\n  - {name: apps, type: select, proxies: [removed-node]}\n")...)
	err := validateRuleTargets(dir, "work", candidate)
	if err == nil || errors.Is(err, errRuleConflict) || applyFailureReason(err) != "activation_failed" {
		t.Fatalf("incorrect classification: %v", err)
	}
	var message *i18n.Message
	if !errors.As(err, &message) || !i18n.ValidDescriptor(message.Descriptor()) {
		t.Fatalf("lost descriptor: %v", err)
	}
	if got := i18n.New(i18n.SimplifiedChinese).Render(message); got != "应用订阅失败：\"apps\" 引用的出站不存在：\"removed-node\"" {
		t.Fatal(got)
	}
	if strings.Contains(err.Error(), "custom rule") || !strings.Contains(err.Error(), "removed-node") {
		t.Fatal(err)
	}
	// The dependency error must survive the wrapper for localized presentation.
	if len(message.Descriptor().ArgumentMessages) != 1 {
		t.Fatal("dependency descriptor lost")
	}
	for path, old := range before {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != old {
			t.Fatalf("preflight changed %s: %v", path, err)
		}
	}
}
