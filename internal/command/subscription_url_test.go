package command

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestSubscriptionAddInitialOptionsOffline(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	var out, stderr bytes.Buffer
	args := []string{"--json", "subscription", "add", "work", "https://feed.invalid/secret", "--schedule", "off", "--test-url", "https://test.invalid/private?token=PRIVATE"}
	if code := Run(args, &out, &stderr, "test", "commit"); code != 0 {
		t.Fatal(code, stderr.String())
	}
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), nil)
	entry, err := subs.Find("work")
	if err != nil || entry.ScheduleSeconds != 0 || !entry.NextDue.IsZero() || entry.HealthcheckURL == nil || *entry.HealthcheckURL != "https://test.invalid/private?token=PRIVATE" {
		t.Fatal(entry, err)
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"--json", "subscription", "list"}, &out, &stderr, "test", "commit"); code != 0 || strings.Contains(out.String(), "PRIVATE") || !strings.Contains(out.String(), `"healthcheck_url":"https://test.invalid"`) {
		t.Fatal(code, out.String(), stderr.String())
	}
	if _, err = os.Stat(filepath.Join(paths.ConfigDir, "profiles")); !os.IsNotExist(err) {
		t.Fatal("offline creation created profile", err)
	}
	for _, tail := range [][]string{{"--schedule", "0"}, {"--schedule", "31536001"}, {"--schedule"}, {"--test-url", "https://user:password@test.invalid/"}, {"--test-url", "relative"}, {"--test-url", "https://test.invalid/#fragment"}, {"--schedule", "off", "--schedule", "45"}, {"--unknown", "x"}} {
		out.Reset()
		stderr.Reset()
		bad := append([]string{"--json", "subscription", "add", "bad", "https://feed.invalid/"}, tail...)
		if code := Run(bad, &out, &stderr, "test", "commit"); code != 2 {
			t.Fatal(bad, code, stderr.String())
		}
	}
	if _, err = subs.Find("bad"); err == nil {
		t.Fatal("invalid creation saved metadata")
	}
}

func TestSubscriptionURLValidationRollback(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), nil)
	if err := subs.Add("feed", "https://feed.invalid/"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	profiles := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil)
	source := []byte("proxy-groups: [{name: Auto, type: url-test, proxies: [DIRECT]}]\nrules: ['MATCH,DIRECT']\n")
	if err := profiles.Write(ctx, "work", source); err != nil {
		t.Fatal(err)
	}
	if err := subs.RecordApplySuccess("feed", "work", "cache", source); err != nil {
		t.Fatal(err)
	}
	old := "https://old.invalid/check"
	if err := subs.SetHealthcheckURL("feed", &old); err != nil {
		t.Fatal(err)
	}
	profiles.Validate = func([]byte) error { return errors.New("reject candidate") }
	candidate := "https://new.invalid/private"
	_, err := applyTransaction(ctx, paths, []string{"proxy", "healthcheck", "url", "set", "subscription", "feed", candidate}, func() (any, error) { return mutateSubscriptionHealthcheckURL(ctx, subs, profiles, "feed", &candidate) })
	after, readErr := subs.Find("feed")
	if err == nil || readErr != nil || after.HealthcheckURL == nil || *after.HealthcheckURL != old {
		t.Fatal(after, err, readErr)
	}
	data, err := profiles.Effective("work")
	if err != nil || !strings.Contains(string(data), "https://old.invalid/check") || strings.Contains(string(data), "new.invalid") {
		t.Fatal(string(data), err)
	}
}
