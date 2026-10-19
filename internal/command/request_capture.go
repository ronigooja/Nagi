package command

import (
	"context"
	"errors"
	"fmt"
	"github.com/ronigooja/Nagi/internal/capture"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/proxy"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	"github.com/ronigooja/Nagi/internal/rewritedraft"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type captureInputKey struct{}
type captureOutputKey struct{}

func captureFailure(code string) error {
	return fail("capture_error", fmt.Errorf("%s; run `nagi rewrite capture status`", code))
}
func validateCaptureArguments(o rewriteArguments) error {
	if o.profile != "" {
		return errors.New("capture uses the selected profile; --profile is not accepted")
	}
	action := strings.TrimPrefix(o.path, "request-rewrite capture ")
	switch action {
	case "body":
		if _, e := strconv.ParseUint(o.values[2], 10, 63); e != nil {
			return errors.New("OFFSET must be a nonnegative byte offset")
		}
		if len(o.values) > 3 {
			n, e := strconv.Atoi(o.values[3])
			if e != nil || n < 1 || n > capture.MaxChunk {
				return errors.New("LIMIT must be 1..262144")
			}
		}
		switch o.values[1] {
		case "request_original", "request_actual", "response_original", "response_actual", "message_original", "message_actual":
		default:
			return errors.New("unknown snapshot")
		}
	case "export":
		if len(o.values) > 1 && o.values[1] != "native" && o.values[1] != "har" {
			return errors.New("format must be native or har")
		}
	case "fixture":
		switch o.values[1] {
		case "request", "response", "exchange", "websocket_client", "websocket_server":
		default:
			return errors.New("unknown fixture stage")
		}
	}
	for _, v := range o.values {
		if strings.ContainsAny(v, "\x00\r\n") {
			return errors.New("invalid argument")
		}
	}
	return nil
}
func captureSocket(p nagiruntime.Paths) string { return filepath.Join(p.RuntimeDir, "capture.sock") }
func captureCall(ctx context.Context, p nagiruntime.Paths, q capture.Request) (capture.Reply, error) {
	r, e := capture.Call(ctx, captureSocket(p), q)
	if e != nil {
		return r, captureFailure(e.Error())
	}
	return r, nil
}
func captureStatus(ctx context.Context, p nagiruntime.Paths) *capture.Status {
	r, e := capture.Call(ctx, captureSocket(p), capture.Request{Action: "status"})
	if e != nil {
		return nil
	}
	return r.Status
}
func capturePolicy(s *capture.Status) *capture.Policy {
	if s == nil {
		return nil
	}
	scope := s.Scope
	scope.Domains = append([]string{}, scope.Domains...)
	scope.ProcessPaths = append([]string{}, scope.ProcessPaths...)
	scope.AppPaths = append([]string{}, scope.AppPaths...)
	return &capture.Policy{SessionID: s.SessionID, Revision: s.Revision, State: s.State, Scope: scope}
}
func captureActive(ctx context.Context, p nagiruntime.Paths) *capture.Status {
	s := captureStatus(ctx, p)
	if s == nil || s.ReadOnly || s.State == "stopped" {
		return nil
	}
	name, e := rewriteSelected(p)
	backend, be := readBackend(p)
	if e != nil || be != nil || s.Profile != name || s.Backend != backend {
		return nil
	}
	return s
}
func captureCommand(ctx context.Context, o rewriteArguments, p nagiruntime.Paths) (any, error) {
	action := strings.TrimPrefix(o.path, "request-rewrite capture ")
	data, _ := ctx.Value(rewriteInputKey{}).([]byte)
	q := capture.Request{Action: action}
	switch action {
	case "start":
		options := capture.StartOptions{Owner: "cli"}
		if len(data) > 0 {
			if rewriteStrictJSON(data, &options) != nil {
				return nil, captureFailure("capture_options_invalid")
			}
		}
		name, e := rewriteSelected(p)
		if e != nil {
			return nil, captureFailure("capture_profile_required")
		}
		options.Profile = name
		options.Backend, e = readBackend(p)
		if e != nil {
			return nil, e
		}
		if options.Owner == "" {
			options.Owner = "cli"
		}
		snapshot, e := (rewrite.Store{ConfigDir: p.ConfigDir, Profile: name}).Load()
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		if _, _, e = rewriteCapturePrerequisites(ctx, p, snapshot); e != nil {
			return nil, e
		}
		if e = ensureCaptureService(ctx, p); e != nil {
			return nil, e
		}
		q.Start = &options
		r, e := captureCall(ctx, p, q)
		if e != nil {
			return nil, e
		}
		if e = rewriteActivate(ctx, p, name, snapshot); e != nil {
			_, _ = capture.Call(ctx, captureSocket(p), capture.Request{Action: "stop", SessionID: r.Status.SessionID})
			return nil, e
		}
		if e = capturePublish(ctx, p, *r.Status); e != nil {
			_, _ = capture.Call(ctx, captureSocket(p), capture.Request{Action: "stop", SessionID: r.Status.SessionID})
			return nil, e
		}
		return output.CaptureResult{Value: r}, nil
	case "pause", "resume", "stop":
		if action == "resume" {
			name, e := rewriteSelected(p)
			if e != nil {
				return nil, captureFailure("capture_profile_required")
			}
			snapshot, e := (rewrite.Store{ConfigDir: p.ConfigDir, Profile: name}).Load()
			if e != nil {
				return nil, rewriteDomainError(e)
			}
			if _, _, e = rewriteCapturePrerequisites(ctx, p, snapshot); e != nil {
				return nil, e
			}
			if _, e = readRewriteRun(p); e != nil {
				return nil, captureFailure("capture_runtime_unavailable")
			}
		}
		q.SessionID = o.values[0]
		r, e := captureCall(ctx, p, q)
		if e != nil {
			return nil, e
		}
		if e = capturePublish(ctx, p, *r.Status); e != nil {
			return nil, e
		}
		return output.CaptureResult{Value: r}, nil
	case "clear":
		q.SessionID = o.values[0]
	case "list":
		if len(data) > 0 {
			q.List = &capture.ListOptions{}
			if rewriteStrictJSON(data, q.List) != nil {
				return nil, captureFailure("capture_options_invalid")
			}
		}
	case "show":
		q.RecordID = o.values[0]
	case "body":
		q.RecordID = o.values[0]
		q.Snapshot = o.values[1]
		q.Offset, _ = strconv.ParseInt(o.values[2], 10, 64)
		q.Limit = capture.MaxChunk
		if len(o.values) > 3 {
			q.Limit, _ = strconv.Atoi(o.values[3])
		}
	case "export":
		return captureExportCommand(ctx, p, o.values)
	case "import":
		if e := ensureCaptureService(ctx, p); e != nil {
			return nil, e
		}
		s, e := captureImportInput(ctx, p, o.values[0])
		if e != nil {
			return nil, captureFailure(e.Error())
		}
		return output.CaptureResult{Value: capture.Reply{Sensitive: true, Status: &s}}, nil
	case "apps":
		return captureApps(ctx, p)
	case "fixture", "draft":
		record, bodies, e := captureRecord(ctx, p, o.values[0])
		if e != nil {
			return nil, e
		}
		var parents []rewritedraft.Context
		if record.ParentID != "" {
			parent, pb, e := captureRecord(ctx, p, record.ParentID)
			if e != nil {
				return nil, e
			}
			parents = append(parents, rewritedraft.Context{Record: parent, Bodies: pb})
		}
		if action == "fixture" {
			v, e := rewritedraft.Fixture(record, bodies, o.values[1], parents...)
			if e != nil {
				return nil, rewriteDomainError(e)
			}
			return output.CaptureResult{Value: v}, nil
		}
		selection, e := rewritedraft.ParseSelection(data)
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		if len(parents) > 0 {
			selection.Context = &parents[0]
		}
		name, e := rewriteSelected(p)
		if e != nil {
			return nil, e
		}
		snapshot, e := (rewrite.Store{ConfigDir: p.ConfigDir, Profile: name}).Load()
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		d, e := rewritedraft.FromRecord(record, bodies, selection, snapshot)
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		return output.CaptureResult{Value: d}, nil
	}
	r, e := captureCall(ctx, p, q)
	if e != nil {
		return nil, e
	}
	return output.CaptureResult{Value: r}, nil
}
func captureRecord(ctx context.Context, p nagiruntime.Paths, id string) (capture.Record, map[string][]byte, error) {
	r, e := captureCall(ctx, p, capture.Request{Action: "show", RecordID: id})
	if e != nil || r.Record == nil {
		return capture.Record{}, nil, captureFailure("capture_record_unavailable")
	}
	bodies := map[string][]byte{}
	for name, s := range r.Record.Snapshots {
		if !strings.HasSuffix(name, "_original") || s.Captured > rewrite.MaxBodyBytes {
			continue
		}
		var offset int64
		for {
			chunk, e := captureCall(ctx, p, capture.Request{Action: "body", RecordID: id, Snapshot: name, Offset: offset, Limit: capture.MaxChunk})
			if e != nil {
				return capture.Record{}, nil, e
			}
			bodies[name] = append(bodies[name], chunk.Body.Data...)
			if chunk.Body.EOF {
				break
			}
			if chunk.Body.NextOffset <= offset {
				return capture.Record{}, nil, captureFailure("capture_body_invalid")
			}
			offset = chunk.Body.NextOffset
		}
	}
	return *r.Record, bodies, nil
}
func captureExportCommand(ctx context.Context, p nagiruntime.Paths, args []string) (any, error) {
	format := "native"
	if len(args) > 1 {
		format = args[1]
	}
	if args[0] == "-" {
		writer, ok := ctx.Value(captureOutputKey{}).(io.Writer)
		if !ok {
			return nil, captureFailure("capture_export_failed")
		}
		if e := capture.Export(ctx, captureSocket(p), writer, format); e != nil {
			return nil, captureFailure(e.Error())
		}
		return output.Raw(nil), nil
	}
	f, e := os.OpenFile(args[0], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, captureFailure("capture_export_failed")
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(args[0])
		}
	}()
	if e = capture.Export(ctx, captureSocket(p), f, format); e != nil {
		return nil, captureFailure(e.Error())
	}
	if e = f.Sync(); e != nil {
		return nil, captureFailure("capture_export_failed")
	}
	if e = f.Close(); e != nil {
		return nil, captureFailure("capture_export_failed")
	}
	ok = true
	return output.CaptureResult{Value: map[string]any{"exported": true, "path": args[0], "sensitive": true}}, nil
}
func rewriteDraftCommand(ctx context.Context, o rewriteArguments, p nagiruntime.Paths, store rewrite.Store, snapshot *rewrite.Snapshot, data []byte) (any, error) {
	d, e := rewritedraft.ParseDraft(data)
	if e != nil {
		return nil, rewriteDomainError(e)
	}
	if strings.HasSuffix(o.path, "preview") {
		v, e := rewritedraft.Preview(ctx, snapshot, d)
		if e != nil {
			return nil, rewriteDomainError(e)
		}
		return output.CaptureResult{Value: v}, nil
	}
	candidate, e := rewritedraft.Apply(snapshot, d)
	if e != nil {
		return nil, rewriteDomainError(e)
	}
	saved, e := store.SaveVersion(ctx, candidate, d.BaseRuleVersion)
	if e != nil {
		return nil, rewriteDomainError(e)
	}
	return output.CaptureResult{Value: saved}, nil
}

func captureImportInput(ctx context.Context, p nagiruntime.Paths, path string) (capture.Status, error) {
	reader, _ := ctx.Value(captureInputKey{}).(io.Reader)
	if path != "-" {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return capture.Status{}, errors.New("capture_private_file_required")
		}
		f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if e != nil {
			return capture.Status{}, errors.New("capture_input_unavailable")
		}
		defer f.Close()
		actual, e := f.Stat()
		if e != nil || !os.SameFile(info, actual) || !actual.Mode().IsRegular() || actual.Mode().Perm()&0077 != 0 {
			return capture.Status{}, errors.New("capture_private_file_required")
		}
		owner, ok := actual.Sys().(*syscall.Stat_t)
		if !ok || int(owner.Uid) != os.Getuid() {
			return capture.Status{}, errors.New("capture_private_file_required")
		}
		reader = f
	}
	if reader == nil {
		return capture.Status{}, errors.New("capture_input_unavailable")
	}
	return capture.Import(ctx, captureSocket(p), reader)
}

func captureApps(ctx context.Context, p nagiruntime.Paths) (any, error) {
	apps := []map[string]string{}
	seen := map[string]bool{}
	add := func(process, path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		app := ""
		if i := strings.Index(path, ".app/"); i >= 0 {
			app = path[:i+4]
		}
		apps = append(apps, map[string]string{"process": process, "process_path": path, "app_path": app})
	}
	cursor := ""
	for {
		r, e := capture.Call(ctx, captureSocket(p), capture.Request{Action: "list", List: &capture.ListOptions{Limit: 200, Cursor: cursor}})
		if e != nil {
			break
		}
		for _, v := range r.Records {
			add(v.Identity.Process, v.Identity.ProcessPath)
		}
		if r.NextCursor == "" {
			break
		}
		cursor = r.NextCursor
	}
	client, e := rewriteClient(p)
	if e == nil {
		connections, ce := proxy.NewService(client).Connections(ctx)
		if ce == nil {
			for _, v := range connections {
				process, _ := v.Metadata["process"].(string)
				path, _ := v.Metadata["processPath"].(string)
				add(process, path)
			}
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i]["process_path"] < apps[j]["process_path"] })
	return output.CaptureResult{Value: map[string]any{"apps": apps, "sensitive": true}}, nil
}
