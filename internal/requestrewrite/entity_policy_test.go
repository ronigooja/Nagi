package requestrewrite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestFinalEntityFailureRetainsContributingRulePolicy(t *testing.T) {
	defer SetScriptExecutor(nil)
	for _, kind := range []string{"body-status", "trailer-status", "status-body", "script-status"} {
		for _, policy := range []string{"block", "forward_original"} {
			t.Run(kind+"/"+policy, func(t *testing.T) {
				d := baseDoc()
				body := Operation{Op: "body_set", Encoding: "base64", Value: str("eA==")}
				status := Operation{Op: "status_set", Value: raw(`204`)}
				trailer := Operation{Op: "trailer_set", Name: "X-Demo", Value: str("example")}
				a := responseRule("entity", body, status)
				a.OnError = policy
				switch kind {
				case "trailer-status":
					a.Operations = []Operation{trailer, status}
				case "status-body":
					d.Rules = []Rule{responseRule("status", status)}
					a.Operations = []Operation{body}
				case "script-status":
					d.Rules = []Rule{responseRule("body", body)}
					a.Operations = []Operation{{Op: "script", Script: "status"}}
					d.Scripts = []Script{{ID: "status", BodyAccess: "none", Source: "fixture"}}
					SetScriptExecutor(func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
						return raw(`{"action":"replace","message":{"status":204}}`), nil
					})
				}
				tail := responseRule("tail", Operation{Op: "header_set", Name: "X-Tail", Value: str("example")})
				tail.OnError = "forward_original"
				if policy == "forward_original" {
					tail.OnError = "block"
				}
				d.Rules = append(d.Rules, a, tail)
				result, err := compiled(t, d).TransformResponse(context.Background(), request(t, ""), response("original"))
				if policy == "block" {
					var failure *Error
					if !errors.As(err, &failure) || failure.RuleID != "entity" {
						t.Fatalf("wrong failure owner: %+v %v", result, err)
					}
				} else {
					if err != nil || result.Outcome != "forward_original" || responseBody(t, result.Response) != "original" || result.Response.Header.Get("X-Tail") != "" {
						t.Fatal(result, err)
					}
				}
			})
		}
	}
}
