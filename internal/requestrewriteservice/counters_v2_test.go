package requestrewriteservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func TestFallbackCountersRetainSanitizedFailureCategories(t *testing.T) {
	defer domain.SetScriptExecutor(nil)
	for _, stage := range []string{domain.StageRequest, domain.StageResponse, domain.StageWebSocketClient} {
		for _, code := range []string{"rewrite_limit", "rewrite_script_timeout", "rewrite_transform"} {
			t.Run(stage+"/"+code, func(t *testing.T) {
				rule := v2Rule("fallback", stage, domain.Operation{Op: "script", Script: "failure"})
				rule.OnError = "forward_original"
				snapshot, err := domain.Compile(domain.Document{Version: 2, Rules: []domain.Rule{rule}, Scripts: []domain.Script{{ID: "failure", BodyAccess: "none", Source: "function transform(i){}"}}})
				if err != nil {
					t.Fatal(err)
				}
				domain.SetScriptExecutor(func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
					return nil, &domain.Error{Code: code}
				})
				req, _ := http.NewRequest("GET", "http://api.service.example/", nil)
				response := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("original"))}
				outcome, failure := "", ""
				switch stage {
				case domain.StageRequest:
					result, e := snapshot.Transform(context.Background(), req)
					err = e
					outcome, failure = result.Outcome, result.FailureCode
					if result.Request != nil && result.Request.Body != nil {
						result.Request.Body.Close()
					}
				case domain.StageResponse:
					result, e := snapshot.TransformResponse(context.Background(), req, response)
					err = e
					outcome, failure = result.Outcome, result.FailureCode
					if result.Response != nil {
						result.Response.Body.Close()
					}
				default:
					response.StatusCode = 101
					result, e := snapshot.TransformMessage(context.Background(), stage, req, response, domain.Message{Type: "text", Body: []byte("original")})
					err = e
					outcome, failure = result.Outcome, result.FailureCode
				}
				if err != nil || outcome != "forward_original" || failure != code {
					t.Fatal(outcome, failure, err)
				}
				service := &Service{}
				service.count(stage, outcome, err, failure)
				counters := service.counters
				if counters.TransformFailures != 1 || counters.Fallbacks != 1 || counters.StageHits != 1 || counters.Rewritten != 0 {
					t.Fatal(counters)
				}
				if (counters.ResourceExhaustion == 1) != (code == "rewrite_limit") || (counters.ScriptTimeouts == 1) != (code == "rewrite_script_timeout") {
					t.Fatal(counters)
				}
			})
		}
	}
}
