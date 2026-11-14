package server

import (
	"testing"
	"time"
)

func TestDNSWatchDelayIsFastOnlyInsideBoostWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	for name, tc := range map[string]struct {
		until time.Time
		want  time.Duration
	}{
		"never boosted":      {time.Time{}, dnsWatchSlow},
		"inside window":      {now.Add(time.Second), dnsWatchFast},
		"exactly at expiry":  {now, dnsWatchSlow},
		"after window ended": {now.Add(-time.Second), dnsWatchSlow},
	} {
		if got := dnsWatchDelay(now, tc.until); got != tc.want {
			t.Errorf("%s: delay = %v, want %v", name, got, tc.want)
		}
	}
}

func TestBoostDNSWatchOpensAndExpiresFastWindow(t *testing.T) {
	h := &RuntimeHandler{}
	before := time.Now()
	h.boostDNSWatch()
	if h.dnsFastUntil.Before(before.Add(dnsWatchBoost)) {
		t.Fatalf("boost ends too early: %v", h.dnsFastUntil)
	}
	if got := dnsWatchDelay(before, h.dnsFastUntil); got != dnsWatchFast {
		t.Fatalf("delay right after boost = %v, want %v", got, dnsWatchFast)
	}
	if got := dnsWatchDelay(h.dnsFastUntil.Add(time.Millisecond), h.dnsFastUntil); got != dnsWatchSlow {
		t.Fatalf("delay after boost expiry = %v, want %v", got, dnsWatchSlow)
	}
}

func TestBoostDNSWatchExtendsAnExistingWindow(t *testing.T) {
	h := &RuntimeHandler{dnsFastUntil: time.Now().Add(time.Second)}
	h.boostDNSWatch()
	if h.dnsFastUntil.Before(time.Now().Add(dnsWatchBoost - time.Second)) {
		t.Fatalf("reload during a boost did not restart the window: %v", h.dnsFastUntil)
	}
}

func TestSteadyStateDNSPollingCostsAtMostOneTenthOfBaseline(t *testing.T) {
	if dnsWatchSlow < 10*dnsWatchFast {
		t.Fatalf("steady interval %v must be at least ten times the boosted interval %v", dnsWatchSlow, dnsWatchFast)
	}
}
