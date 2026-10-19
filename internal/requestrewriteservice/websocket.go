package requestrewriteservice

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
)

// One bounded message workspace is admitted at a time. Directions that have no
// applicable message edit continue streaming and never reserve this workspace.
var messageWorkspace = make(chan struct{}, 1)
var errHandshakeResponse = errors.New("rewrite_handshake_response")

type controlledRoundTripper func(*http.Request) (*http.Response, error)

func (f controlledRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func protocolValues(h http.Header) []string {
	out := []string{}
	for _, value := range h.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(value, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func (s *Service) serveWebSocket(w http.ResponseWriter, req *http.Request, cc *connectionContext, wire net.Conn) {
	if req.Method != http.MethodGet || req.Header.Get("Sec-WebSocket-Version") != "13" {
		writeOriginResponse(w, req, fixedResponse(req, "rewrite_invalid"))
		return
	}
	stripHeaders(req.Header)
	originalRequest := req.Clone(req.Context())
	result, err := s.transformRequest(req, wire, true)
	if err != nil {
		writeOriginResponse(w, req, fixedResponse(req, transformCode(err)))
		return
	}
	outgoing := result.Request.WithContext(req.Context())
	headers := outgoing.Header.Clone()
	for _, key := range []string{"Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol"} {
		headers.Del(key)
	}
	var rejected *http.Response
	var originalHandshake *http.Response
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: controlledRoundTripper(func(r *http.Request) (*http.Response, error) {
		response, err := cc.roundTrip(r)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusSwitchingProtocols {
			rejected = response
			return nil, errHandshakeResponse
		}
		extension := response.Header.Get("Sec-WebSocket-Extensions")
		if extension != "" && (!strings.Contains(extension, "client_no_context_takeover") || !strings.Contains(extension, "server_no_context_takeover")) {
			response.Body.Close()
			return nil, errInvalid
		}
		cloned := *response
		cloned.Header = response.Header.Clone()
		cloned.Trailer = response.Trailer.Clone()
		cloned.Body = http.NoBody
		originalHandshake = &cloned
		return response, nil
	})}
	upstream, _, err := websocket.Dial(cc.ctx, outgoing.URL.String(), &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers, Subprotocols: protocolValues(outgoing.Header), CompressionMode: websocket.CompressionNoContextTakeover})
	if err != nil {
		if rejected != nil {
			response, failure := s.transformResponse(originalRequest, rejected, wire)
			if failure != nil {
				response = fixedResponse(req, transformCode(failure))
			}
			writeOriginResponse(w, req, response)
		} else {
			code := "rewrite_unavailable"
			if errors.Is(err, errInvalid) {
				code = "rewrite_invalid"
			}
			writeOriginResponse(w, req, fixedResponse(req, code))
		}
		return
	}
	defer upstream.CloseNow()
	response, err := s.transformResponse(originalRequest, originalHandshake, wire)
	if err == nil {
		response, err = s.snapshot.TransformHandshake(originalRequest, originalHandshake, response)
	}
	if err != nil {
		writeOriginResponse(w, req, fixedResponse(req, transformCode(err)))
		return
	}
	stripHeaders(response.Header)
	for k, values := range response.Header {
		switch strings.ToLower(k) {
		case "connection", "upgrade", "sec-websocket-accept", "sec-websocket-protocol", "sec-websocket-extensions", "content-length", "transfer-encoding":
			continue
		}
		w.Header()[k] = append([]string(nil), values...)
	}
	protocols := []string{}
	if p := upstream.Subprotocol(); p != "" {
		protocols = append(protocols, p)
	}
	downstream, err := websocket.Accept(w, req, &websocket.AcceptOptions{Subprotocols: protocols, CompressionMode: websocket.CompressionNoContextTakeover, InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer downstream.CloseNow()
	ctx, cancel := context.WithCancel(cc.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { upstream.CloseNow(); downstream.CloseNow() })
	defer stop()
	clientEdit := s.snapshot.NeedsMessageEditing(domain.StageWebSocketClient, originalRequest, originalHandshake)
	serverEdit := s.snapshot.NeedsMessageEditing(domain.StageWebSocketServer, originalRequest, originalHandshake)
	if clientEdit {
		downstream.SetReadLimit(domain.MaxBodyBytes)
	} else {
		downstream.SetReadLimit(-1)
	}
	if serverEdit {
		upstream.SetReadLimit(domain.MaxBodyBytes)
	} else {
		upstream.SetReadLimit(-1)
	}
	type outcome struct {
		err         error
		destination *websocket.Conn
	}
	done := make(chan outcome, 2)
	go func() {
		done <- outcome{s.relayMessages(ctx, downstream, upstream, domain.StageWebSocketClient, originalRequest, originalHandshake, clientEdit), upstream}
	}()
	go func() {
		done <- outcome{s.relayMessages(ctx, upstream, downstream, domain.StageWebSocketServer, originalRequest, originalHandshake, serverEdit), downstream}
	}()
	first := <-done
	code := websocket.CloseStatus(first.err)
	if code != websocket.StatusNormalClosure && code != websocket.StatusGoingAway {
		s.mu.Lock()
		s.counters.WebSocketAbnormalCloses++
		s.mu.Unlock()
	}
	if code < 1000 || code == websocket.StatusNoStatusRcvd || code == websocket.StatusAbnormalClosure || code == websocket.StatusTLSHandshake {
		code = websocket.StatusInternalError
	}
	_ = first.destination.Close(code, "")
	cancel()
	<-done
}

func (s *Service) relayMessages(ctx context.Context, source, destination *websocket.Conn, stage string, req *http.Request, handshake *http.Response, editing bool) error {
	for {
		kind, reader, err := source.Reader(ctx)
		if err != nil {
			return err
		}
		if !editing {
			writer, err := destination.Writer(ctx, kind)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(writer, reader)
			closeErr := writer.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			continue
		}
		msgCtx, cancel := context.WithTimeout(ctx, s.transformTimeout)
		stop := context.AfterFunc(msgCtx, func() { source.CloseNow() })
		select {
		case messageWorkspace <- struct{}{}:
		case <-msgCtx.Done():
			stop()
			cancel()
			return errUnavailable
		}
		body, err := io.ReadAll(io.LimitReader(reader, domain.MaxBodyBytes+1))
		if websocket.CloseStatus(err) == websocket.StatusMessageTooBig {
			err = errLimit
		}
		if err == nil && len(body) > domain.MaxBodyBytes {
			err = errLimit
		}
		var result domain.MessageResult
		if err == nil {
			messageType := "binary"
			if kind == websocket.MessageText {
				messageType = "text"
			}
			result, err = s.snapshot.TransformMessage(msgCtx, stage, req, handshake, domain.Message{Type: messageType, Body: body})
		}
		expired := msgCtx.Err() != nil
		stopped := stop()
		cancel()
		if err == nil && (expired || !stopped) {
			err = errUnavailable
		}
		s.count(stage, result.Outcome, err)
		if err == nil {
			kind = websocket.MessageBinary
			if result.Message.Type == "text" {
				kind = websocket.MessageText
			}
			err = destination.Write(ctx, kind, result.Message.Body)
		}
		<-messageWorkspace
		if err != nil {
			return err
		}
	}
}
