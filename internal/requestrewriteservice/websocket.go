package requestrewriteservice

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/coder/websocket"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
)

// One bounded message workspace is admitted at a time. Directions that have no
// applicable message edit continue streaming and never reserve this workspace.
var messageWorkspace = make(chan struct{}, 1)
var errHandshakeResponse = errors.New("rewrite_handshake_response")
var errTextUTF8 = errors.New("rewrite_websocket_utf8")

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
	cap := s.beginCapture(cc, req, "http", "", "", 0)
	req = req.WithContext(context.WithValue(req.Context(), captureKey{}, cap))
	if cap != nil {
		req.Body = cap.body("request_original", req.Body, func() http.Header { return req.Trailer })
	}
	stripHeaders(req.Header)
	originalRequest := req.Clone(req.Context())
	result, err := s.transformRequest(req, wire, true)
	if err != nil {
		writeOriginResponse(w, req, fixedResponse(req, transformCode(err)))
		return
	}
	outgoing := result.Request.WithContext(req.Context())
	if cap != nil {
		cap.requestActual(outgoing, result.RuleIDs, result.Outcome)
		outgoing.Body = cap.body("request_actual", outgoing.Body, func() http.Header { return outgoing.Trailer })
	}
	// Dial creates its own empty handshake body; release any buffered transaction.
	if outgoing.Body != nil {
		_ = outgoing.Body.Close()
	}
	if outgoing.Method != http.MethodGet || outgoing.ContentLength != 0 || len(outgoing.TransferEncoding) != 0 || len(outgoing.Trailer) != 0 {
		writeOriginResponse(w, req, fixedResponse(req, "rewrite_invalid"))
		return
	}
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
		if cap != nil {
			cap.response("response_original", response)
		}
		if response.StatusCode != http.StatusSwitchingProtocols {
			if cap != nil {
				response.Body = cap.body("response_original", response.Body, func() http.Header { return response.Trailer })
			}
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
			if cap != nil && response != nil {
				cap.response("response_actual", response)
				response.Body = cap.body("response_actual", response.Body, func() http.Header { return response.Trailer })
				cap.complete("complete", "", 0)
			}
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
	if cap != nil {
		cap.offer(captureFinish("response_original"))
	}
	response, err := s.transformResponse(originalRequest, originalHandshake, wire)
	if err == nil {
		response, err = s.snapshot.TransformHandshake(originalRequest, originalHandshake, response)
	}
	if err != nil {
		writeOriginResponse(w, req, fixedResponse(req, transformCode(err)))
		return
	}
	if cap != nil {
		cap.response("response_actual", response)
		cap.offer(captureFinish("response_actual"))
		cap.complete("upgraded", "", 0)
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
	if cap != nil {
		ctx = context.WithValue(ctx, websocketCaptureKey{}, websocketCapture{cc: cc, parent: cap.record.ID})
	}
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
	if cap != nil {
		cap.complete("closed", "", int(code))
	}
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
	var sequence uint64
	for {
		kind, reader, err := source.Reader(ctx)
		if err != nil {
			return err
		}
		sequence++
		var cap *captureRecord
		messageType := "binary"
		if kind == websocket.MessageText {
			messageType = "text"
		}
		if info, ok := ctx.Value(websocketCaptureKey{}).(websocketCapture); ok {
			cap = s.beginCapture(info.cc, req, "websocket", info.parent, stage, sequence)
		}
		var originalBody, actualBody *captureBody
		if cap != nil {
			originalBody = cap.messageSnapshot("message_original", messageType)
			reader = &captureReader{Reader: reader, body: originalBody}
		}
		if !editing {
			writer, err := destination.Writer(ctx, kind)
			if err != nil {
				return err
			}
			var target io.Writer = writer
			if cap != nil {
				actualBody = cap.messageSnapshot("message_actual", messageType)
				target = captureWriter{Writer: writer, body: actualBody}
			}
			var copyErr error
			if kind == websocket.MessageText {
				copyErr = copyTextMessage(target, reader)
			} else {
				_, copyErr = io.Copy(target, reader)
			}
			if cap != nil {
				state := "complete"
				if copyErr != nil {
					state = "failed"
				}
				originalBody.finish(state)
				actualBody.finish(state)
				cap.complete("unchanged", "", 0)
			}
			if copyErr != nil {
				// Do not finish a partial invalid message with a final data frame.
				if errors.Is(copyErr, errTextUTF8) {
					return closeInvalidText(source)
				}
				return copyErr
			}
			closeErr := writer.Close()
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
		if err == nil && kind == websocket.MessageText && !utf8.Valid(body) {
			err = errTextUTF8
		}
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
		s.count(stage, result.Outcome, err, result.FailureCode)
		if cap != nil {
			originalBody.finish("complete")
			cap.result(result.RuleIDs, result.Outcome)
		}
		if err == nil {
			kind = websocket.MessageBinary
			if result.Message.Type == "text" {
				kind = websocket.MessageText
			}
			if cap != nil {
				actualBody = cap.messageSnapshot("message_actual", result.Message.Type)
			}
			err = destination.Write(ctx, kind, result.Message.Body)
			if cap != nil {
				if err == nil {
					actualBody.append(result.Message.Body)
					actualBody.finish("complete")
					cap.complete(result.Outcome, "", 0)
				} else {
					actualBody.finish("failed")
					cap.complete("failed", "websocket_failed", 0)
				}
			}
		}
		<-messageWorkspace
		if err != nil {
			if cap != nil {
				originalBody.finish("failed")
				cap.complete("failed", "websocket_failed", 0)
			}
			if errors.Is(err, errTextUTF8) {
				return closeInvalidText(source)
			}
			return err
		}
	}
}

// copyTextMessage validates complete UTF-8 runes before forwarding them, keeping
// at most one incomplete rune between reads. It preserves streaming/backpressure.
func copyTextMessage(dst io.Writer, src io.Reader) error {
	var buffer [32 * 1024]byte
	pending := 0
	for {
		n, readErr := src.Read(buffer[pending:])
		size := pending + n
		valid := 0
		for valid < size {
			if !utf8.FullRune(buffer[valid:size]) {
				break
			}
			r, width := utf8.DecodeRune(buffer[valid:size])
			if r == utf8.RuneError && width == 1 {
				return errTextUTF8
			}
			valid += width
		}
		if valid > 0 {
			written, err := dst.Write(buffer[:valid])
			if err != nil {
				return err
			}
			if written != valid {
				return io.ErrShortWrite
			}
		}
		pending = copy(buffer[:], buffer[valid:size])
		if readErr != nil {
			if readErr == io.EOF {
				if pending != 0 {
					return errTextUTF8
				}
				return nil
			}
			return readErr
		}
	}
}
func closeInvalidText(source *websocket.Conn) error {
	_ = source.Close(websocket.StatusInvalidFramePayloadData, "")
	return websocket.CloseError{Code: websocket.StatusInvalidFramePayloadData}
}
