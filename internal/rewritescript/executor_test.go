package rewritescript

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if err := Prepare(context.Background()); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestTransform(t *testing.T) {
	got, err := Execute(context.Background(), `function transform(x) { return {action:"replace",message:{value:x.value+1}} }`, json.RawMessage(`{"value":4}`))
	if err != nil || string(got) != `{"action":"replace","message":{"value":5}}` {
		t.Fatalf("%s %v", got, err)
	}
}
func TestNoCapabilitiesAndFreshState(t *testing.T) {
	source := `function transform() { if (typeof Date !== 'undefined' || typeof Math.random !== 'undefined' || typeof fetch !== 'undefined' || typeof process !== 'undefined' || typeof require !== 'undefined' || typeof std !== 'undefined' || typeof os !== 'undefined' || typeof console !== 'undefined') throw Error('capability'); globalThis.n=(globalThis.n||0)+1; return {action:'replace',message:{n:globalThis.n,keys:Object.keys({b:1,a:2})}} }`
	var first []byte
	for i := 0; i < 4; i++ {
		got, err := Execute(context.Background(), source, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = got
		} else if !bytes.Equal(first, got) {
			t.Fatal("state or nondeterminism")
		}
	}
	if !bytes.Contains(first, []byte(`"n":1`)) {
		t.Fatal(string(first))
	}
}
func TestLimitsAndSanitizedFailures(t *testing.T) {
	for _, source := range []string{
		`throw Error('private-source-body')`,
		`function transform(){throw Error('private-source-body')}`,
		`function transform(){return undefined}`,
		`function transform(){let a={};a.a=a;return a}`,
		`import x from 'file:///etc/passwd'; function transform(){return x}`,
		`function transform(){ return {action:'replace', message:'x'.repeat(13*1024*1024)} }`,
		`function transform(){let a=[]; for(;;) a.push(new Uint8Array(1024*1024));}`,
	} {
		_, err := Execute(context.Background(), source, json.RawMessage(`{}`))
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("unexpected error %v", err)
		}
	}
	for _, source := range []string{`function transform(){return null}`, `function transform(){return []}`, `function transform(){return {action:'unknown'}}`, `async function transform(){return {action:'block'}}`} {
		_, err := Execute(context.Background(), source, json.RawMessage(`{}`))
		if !errors.Is(err, ErrOutput) {
			t.Fatalf("invalid result %v", err)
		}
	}
	_, err := Execute(context.Background(), strings.Repeat(" ", MaxSourceBytes+1), json.RawMessage(`{}`))
	if !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), `function transform(){}`, json.RawMessage(strings.Repeat(" ", MaxInputBytes+1)))
	if !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}
func TestTimeoutCancellationAndRecovery(t *testing.T) {
	if err := Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := Execute(context.Background(), `function transform(){for(;;){}}`, json.RawMessage(`{}`))
	if !errors.Is(err, ErrTimeout) || time.Since(start) > 3*time.Second {
		t.Fatalf("%v %s", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(25 * time.Millisecond); cancel() }()
	start = time.Now()
	_, err = Execute(ctx, `function transform(){for(;;){}}`, json.RawMessage(`{}`))
	if !errors.Is(err, ErrCanceled) || time.Since(start) > time.Second {
		t.Fatalf("%v %s", err, time.Since(start))
	}
	_, err = Execute(context.Background(), `function transform(){return {action:'unchanged'}}`, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
}
func TestParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Execute(ctx, `function transform(){for(;;){}}`, json.RawMessage(`{}`))
	if !errors.Is(err, ErrTimeout) || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("%v %s", err, time.Since(start))
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	_, err = Execute(canceled, `function transform(){return {action:'block'}}`, json.RawMessage(`{}`))
	if !errors.Is(err, ErrCanceled) {
		t.Fatal(err)
	}
}

func TestConcurrentAdmission(t *testing.T) {
	if err := Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := Execute(ctx, `function transform(){for(;;){}}`, json.RawMessage(`{}`)); done <- err }()
	}
	deadline := time.Now().Add(time.Second)
	for len(slots) != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	_, err := Execute(context.Background(), `function transform(){return {action:'block'}}`, json.RawMessage(`{}`))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("admission: %v", err)
	}
	cancel()
	for i := 0; i < 2; i++ {
		if err := <-done; !errors.Is(err, ErrCanceled) {
			t.Fatal(err)
		}
	}
	if len(slots) != 0 {
		t.Fatal("leaked admission")
	}
}
func TestLargestBinarySample(t *testing.T) {
	if raceInstrumentation {
		t.Skip("8MiB throughput acceptance runs without race instrumentation; the one-second production deadline is unchanged")
	}
	data := bytes.Repeat([]byte{0, 255, 65, 128}, (8<<20)/4)
	encoded := base64.StdEncoding.EncodeToString(data)
	input, _ := json.Marshal(map[string]any{"stage": "response", "original": map[string]any{"url": "https://example.invalid/"}, "message": map[string]any{"body": map[string]any{"encoding": "base64", "data": encoded}}})
	start := time.Now()
	got, err := Execute(context.Background(), `function transform(x){return {action:'replace',message:x.message}}`, input)
	if err != nil {
		t.Fatalf("maximum legal binary input (%d bytes): %v after %s", len(input), err, time.Since(start))
	}
	var result struct {
		Message struct {
			Body struct {
				Data string `json:"data"`
			} `json:"body"`
		} `json:"message"`
	}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatal(err)
	}
	if result.Message.Body.Data != encoded {
		t.Fatal("binary sample changed")
	}
	t.Logf("8MiB binary roundtrip %s", time.Since(start))
}
