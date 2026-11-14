package command

import "testing"

func TestRuleOptionFlags(t *testing.T) {
	noResolve, src, err := ruleOptionFlags(nil)
	if err != nil || noResolve || src {
		t.Fatal(noResolve, src, err)
	}
	noResolve, src, err = ruleOptionFlags([]string{"--src", "--no-resolve"})
	if err != nil || !noResolve || !src {
		t.Fatal(noResolve, src, err)
	}
	for _, flags := range [][]string{{"--src", "--src"}, {"--no-resolve", "--no-resolve"}, {"--bogus"}, {"no-resolve"}} {
		if _, _, err := ruleOptionFlags(flags); err == nil {
			t.Fatalf("accepted %v", flags)
		}
	}
}

func TestRulesEditRejectsIPOptionsOnDomainRule(t *testing.T) {
	if err := validateInvocation([]string{"rules", "edit", "x", "DOMAIN", "example.com", "DIRECT", "--no-resolve"}); err == nil {
		t.Fatal("domain rule accepted --no-resolve")
	}
	if err := validateInvocation([]string{"rules", "edit", "x", "IP-CIDR", "192.0.2.1/32", "DIRECT", "--no-resolve", "--src"}); err != nil {
		t.Fatal(err)
	}
	if err := validateInvocation([]string{"rules", "add", "x", "IP-CIDR", "192.0.2.1/32", "DIRECT", "--wat"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
	if err := validateInvocation([]string{"rules", "add", "x", "IP-CIDR", "192.0.2.1/32", "DIRECT", "--no-resolve", "--src", "--src"}); err == nil {
		t.Fatal("too many arguments accepted")
	}
}
