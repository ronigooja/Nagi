package menu

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestResourceActionsPaginationAndClamp(t *testing.T) {
	var out bytes.Buffer
	runner := &fakeRunner{}
	loads := 0
	page := &Page{Title: "Resources", Load: func(_ context.Context, _ *Session) (PageView, error) {
		loads++
		view := PageView{Actions: []Item{{Label: "Fixed action", Args: []string{"change"}}}}
		limit := 21
		if loads > 1 {
			limit = 11
		}
		for i := 1; i <= limit; i++ {
			name := fmt.Sprintf("Name %02d", i)
			view.Items = append(view.Items, Item{Name: name, Label: name + " metadata", Args: []string{"pick", name}})
		}
		return view, nil
	}}
	RunPage(context.Background(), NewSession(runner, strings.NewReader("12\n12\n2\n1\n3\n1\n0\n"), &out), page)
	want := [][]string{{"change"}, {"pick", "Name 11"}, {"pick", "Name 01"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls=%v", runner.calls)
	}
	if strings.Count(out.String(), "Fixed action") < 4 {
		t.Fatal("fixed action disappeared during pagination")
	}
	if !strings.Contains(out.String(), "Page 2/2") {
		t.Fatal("out of range page did not clamp to last page")
	}
}

func TestInvalidInputDoesNotReprintPage(t *testing.T) {
	var out bytes.Buffer
	page := &Page{Title: "Once", Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Items: []Item{{Label: "Resource"}}}, nil
	}}
	RunPage(context.Background(), NewSession(&fakeRunner{}, strings.NewReader("\nwrong\n0\nq\n"), &out), page)
	if strings.Count(out.String(), "\nOnce\n") != 1 {
		t.Fatal(out.String())
	}
}

func TestLiteralTextAndFormPreservedRetry(t *testing.T) {
	var out bytes.Buffer
	values := map[string]string{}
	calls := []map[string]string{}
	s := NewSession(&fakeRunner{}, strings.NewReader("\nb\nq\n1\ncorrect\n3\n"), &out)
	result, err := s.Form(context.Background(), []Field{{Key: "name", Label: "Name", Required: true}, {Key: "path", Label: "Path", Required: true}}, values, func(v map[string]string) Result {
		copy := map[string]string{}
		for k, value := range v {
			copy[k] = value
		}
		calls = append(calls, copy)
		if len(calls) == 1 {
			return Result{Code: 1, Error: &CLIError{Message: "failed"}}
		}
		return Result{}
	})
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if !reflect.DeepEqual(calls, []map[string]string{{"name": "b", "path": "q"}, {"name": "correct", "path": "q"}}) {
		t.Fatalf("calls=%v", calls)
	}
	if !strings.Contains(out.String(), "This field is required.") {
		t.Fatal(out.String())
	}
}

func TestFormEOFAfterFailureDoesNotRetry(t *testing.T) {
	calls := 0
	s := NewSession(&fakeRunner{}, strings.NewReader("value\n"), &bytes.Buffer{})
	_, err := s.Form(context.Background(), []Field{{Key: "name", Label: "Name", Required: true}}, nil, func(map[string]string) Result { calls++; return Result{Code: 1} })
	if err != ErrQuit || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestActionNavigationAndNoConfirmation(t *testing.T) {
	var out bytes.Buffer
	calls := 0
	child := &Page{Title: "Child", Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Actions: []Item{{Label: "Delete", Impact: "Deletes the resource", Navigate: func(context.Context, *Session) (Navigation, error) { calls++; return Navigation{Back: true}, nil }}}}, nil
	}}
	root := &Page{Title: "Parent", Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Items: []Item{{Label: "Child", Next: func() *Page { return child }}}}, nil
	}}
	RunPage(context.Background(), NewSession(&fakeRunner{}, strings.NewReader("1\n1\n0\n"), &out), root)
	if calls != 1 || strings.Count(out.String(), "\nParent\n") != 2 || strings.Contains(out.String(), "Type yes") {
		t.Fatal(out.String())
	}
}

func TestMenuResultSummaryPreservesURLContextAndRawConfiguration(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
	entries := make([]string, 100)
	for i := range entries {
		entries[i] = "https://secret.example/token"
	}
	s.ShowResult([]string{"example", "list"}, Result{Data: map[string]any{"added": entries}})
	if strings.Count(out.String(), "https://secret.example/token") != 10 || !strings.Contains(out.String(), "100 entries") {
		t.Fatal(out.String())
	}
	out.Reset()
	s.ShowResult([]string{"config", "show"}, Result{Data: map[string]any{"content": "url: https://config.example/value"}})
	if out.String() != "url: https://config.example/value\n" {
		t.Fatal(out.String())
	}
}
