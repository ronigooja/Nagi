package rewritescript

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestIntegrationMaximumSourceAndInputGuest(t *testing.T) {
	if raceInstrumentation {
		t.Skip("real guest one-second execution limit excludes race-instrumented maximum-input host copies")
	}
	if e := Prepare(context.Background()); e != nil {
		t.Fatal(e)
	}
	// Fill the complete 12 MiB JSON input, leaving only the exact field overhead.
	input := json.RawMessage(`{"value":"` + strings.Repeat("x", MaxInputBytes-len(`{"value":""}`)) + `"}`)
	var decoded struct {
		Value string `json:"value"`
	}
	if e := json.Unmarshal(input, &decoded); e != nil {
		t.Fatal(e)
	}
	source := "function transform(input) { if (input.value.length !== " + strconv.Itoa(len(decoded.Value)) + ") throw Error('length'); return {action:'unchanged'}; }"
	source += "/*" + strings.Repeat("x", MaxSourceBytes-len(source)-4) + "*/"
	if len(input) != MaxInputBytes || len(source) != MaxSourceBytes {
		t.Fatal("wrong test limits")
	}
	result, e := Execute(context.Background(), source, input)
	if e != nil || string(result) != `{"action":"unchanged"}` {
		t.Fatal(string(result), e)
	}
	if _, e := Execute(context.Background(), source, append(input, ' ')); !errors.Is(e, ErrLimit) {
		t.Fatal("oversized guest input accepted", e)
	}
	if _, e := Execute(context.Background(), source+" ", input); !errors.Is(e, ErrLimit) {
		t.Fatal("oversized guest source accepted", e)
	}
	if _, e := Execute(context.Background(), "async function transform(input) { return {action:'unchanged'}; }", json.RawMessage(`{}`)); e == nil {
		t.Fatal("Promise accepted as synchronous result")
	}
}
