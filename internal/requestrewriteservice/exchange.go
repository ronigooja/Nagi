package requestrewriteservice

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
)

type exchangeState struct {
	original     *http.Request
	localFailure bool
}

type RewriteCounters struct {
	Requests                uint64 `json:"requests"`
	Responses               uint64 `json:"responses"`
	Messages                uint64 `json:"messages"`
	Rewritten               uint64 `json:"rewritten"`
	StageHits               uint64 `json:"stage_hits"`
	TransformFailures       uint64 `json:"transform_failures"`
	Fallbacks               uint64 `json:"fallbacks"`
	ResourceExhaustion      uint64 `json:"resource_exhaustion"`
	ScriptTimeouts          uint64 `json:"script_timeouts"`
	WebSocketAbnormalCloses uint64 `json:"websocket_abnormal_closes"`
}

func (s *Service) count(stage, outcome string, err error, failureCode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch stage {
	case "request":
		s.counters.Requests++
	case "response":
		s.counters.Responses++
	default:
		s.counters.Messages++
	}
	if err != nil {
		var failure *domain.Error
		if errors.As(err, &failure) {
			failureCode = failure.Code
		} else if errors.Is(err, errLimit) {
			failureCode = "rewrite_limit"
		} else {
			failureCode = "rewrite_transform"
		}
	}
	if failureCode != "" {
		s.counters.TransformFailures++
		if failureCode == "rewrite_limit" {
			s.counters.ResourceExhaustion++
		}
		if failureCode == "rewrite_script_timeout" {
			s.counters.ScriptTimeouts++
		}
	}
	if err != nil {
		return
	}

	if outcome != "unchanged" && outcome != "" {
		s.counters.StageHits++
	}
	if outcome == "rewritten" {
		s.counters.Rewritten++
	}
	if outcome == "forward_original" {
		s.counters.Fallbacks++
	}
}

func transformCode(err error) string {
	var de *domain.Error
	if errors.As(err, &de) {
		switch de.Code {
		case "rewrite_auth", "rewrite_invalid", "rewrite_limit", "rewrite_cancelled", "rewrite_transform", "rewrite_range_conflict":
			return de.Code
		}
	}
	return "rewrite_transform"
}

func (s *Service) transformRequest(req *http.Request, wire net.Conn, upgrade bool) (domain.Result, error) {
	ctx, cancel := context.WithTimeout(req.Context(), s.transformTimeout)
	stop := context.AfterFunc(ctx, func() {
		if req.ProtoMajor == 1 {
			wire.Close()
		}
		if req.Body != nil {
			req.Body.Close()
		}
	})
	var result domain.Result
	var err error
	if upgrade {
		result, err = s.snapshot.TransformUpgrade(ctx, req)
	} else {
		result, err = s.snapshot.Transform(ctx, req)
	}
	expired := ctx.Err() != nil
	stopped := stop()
	cancel()
	if err == nil && (expired || !stopped) {
		err = errUnavailable
	}
	s.count("request", result.Outcome, err, result.FailureCode)
	if err != nil && result.Request != nil && result.Request.Body != nil {
		result.Request.Body.Close()
	}
	return result, err
}

func (s *Service) transformResponse(req *http.Request, resp *http.Response, wire net.Conn) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(req.Context(), s.transformTimeout)
	stop := context.AfterFunc(ctx, func() {
		if req.ProtoMajor == 1 {
			wire.Close()
		}
		if resp.Body != nil {
			resp.Body.Close()
		}
	})
	result, err := s.snapshot.TransformResponse(ctx, req, resp)
	expired := ctx.Err() != nil
	stopped := stop()
	cancel()
	if err == nil && (expired || !stopped) {
		err = errUnavailable
	}
	s.count("response", result.Outcome, err, result.FailureCode)
	if err != nil {
		if resp.Body != nil {
			resp.Body.Close()
		}
		return nil, err
	}
	return result.Response, nil
}

// writeOriginResponse retains trailers while streaming non-upgrade replies from
// the controlled WebSocket handshake transport. Local failures never enter rules.
func writeOriginResponse(w http.ResponseWriter, req *http.Request, resp *http.Response) {
	defer resp.Body.Close()
	stripHeaders(resp.Header)
	for k, values := range resp.Header {
		w.Header()[k] = append([]string(nil), values...)
	}
	if len(resp.Trailer) > 0 {
		keys := make([]string, 0, len(resp.Trailer))
		for k := range resp.Trailer {
			keys = append(keys, k)
		}
		w.Header().Set("Trailer", strings.Join(keys, ", "))
		w.Header().Del("Content-Length")
	}
	w.WriteHeader(resp.StatusCode)
	if req.Method != http.MethodHead {
		if _, err := io.Copy(flushingWriter{w}, resp.Body); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	for k, values := range resp.Trailer {
		w.Header()[k] = append([]string(nil), values...)
	}
}
