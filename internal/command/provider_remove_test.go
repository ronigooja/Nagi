package command

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
)

func TestProviderRemoveStoppedCLI(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	binary := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", binary)
	s := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil)
	if err := s.Write(context.Background(), "default", []byte("mixed-port: 17890\nproxy-groups: []\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProvider(context.Background(), "default", "work", "https://example.com/private", "Japan", 3600); err != nil {
		t.Fatal(err)
	}
	args := []string{"proxy", "provider", "remove", "work"}
	if !configMutation(args) || !transactionalMutation(args) {
		t.Fatal("removal must lock and transact")
	}
	var out, errs bytes.Buffer
	if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit"); code != 0 {
		t.Fatalf("code=%d error=%s", code, errs.String())
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data["removed"] != true || envelope.Data["provider"] != "work" || envelope.Data["profile"] != "default" || len(envelope.Data) != 3 {
		t.Fatalf("output=%s", out.String())
	}
	data, err := s.Effective("default")
	if err != nil || strings.Contains(string(data), "work") {
		t.Fatalf("effective=%s error=%v", data, err)
	}
	out.Reset()
	errs.Reset()
	if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit"); code != 1 || !strings.Contains(errs.String(), "proxy_provider_error") {
		t.Fatalf("code=%d error=%s", code, errs.String())
	}
}

func TestProviderRemoveValidationWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, name := range []string{"", "COMPATIBLE", " bad", "bad\nname"} {
		var out, errs bytes.Buffer
		if code := Run([]string{"--json", "proxy", "provider", "remove", name}, &out, &errs, "test", "commit"); code != 2 {
			t.Fatalf("name=%q code=%d error=%s", name, code, errs.String())
		}
	}
	var out, errs bytes.Buffer
	if code := Run([]string{"proxy", "provider", "remove", "--help"}, &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), "Removes the provider definition") {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
}
