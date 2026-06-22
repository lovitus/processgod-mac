package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lovitus/processgod-mac/internal/api"
)

type Scope string

const (
	ScopeUser   Scope = "user"
	ScopeSystem Scope = "system"
)

type Request struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestID"`
	Method          string          `json:"method"`
	Params          json.RawMessage `json:"params,omitempty"`
}

type RPCError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type Response struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestID"`
	OK              bool            `json:"ok"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           *RPCError       `json:"error,omitempty"`
}

type EventEnvelope struct {
	ProtocolVersion int       `json:"protocolVersion"`
	Event           api.Event `json:"event"`
}

type Peer struct {
	UID uint32
	GID uint32
	PID int
}

type Handler interface {
	Handle(peer Peer, request Request) Response
	Subscribe(peer Peer) (<-chan api.Event, func(), *RPCError)
}

type Server struct {
	SocketPath string
	Scope      Scope
	Handler    Handler
}

func (s *Server) Run(stop <-chan struct{}) error {
	if s.Handler == nil {
		return errors.New("IPC handler is required")
	}
	if err := s.prepareSocketDirectory(); err != nil {
		return err
	}
	_ = os.Remove(s.SocketPath)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: s.SocketPath, Net: "unix"})
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(s.SocketPath)
	}()
	if err := s.setSocketPermissions(); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		<-stop
		_ = listener.Close()
	}()
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				errCh <- err
				return
			}
			go s.handleConn(conn)
		}
	}()

	select {
	case <-stop:
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleConn(conn *net.UnixConn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	peer, err := peerCredentials(conn)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(ErrorResponse("", "peer_credentials", err.Error(), nil))
		return
	}
	if s.Scope == ScopeUser && peer.UID != uint32(os.Geteuid()) {
		_ = json.NewEncoder(conn).Encode(ErrorResponse("", "permission_denied", "socket owner mismatch", nil))
		return
	}
	var request Request
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(ErrorResponse("", "invalid_request", err.Error(), nil))
		return
	}
	if request.ProtocolVersion != api.ProtocolVersion {
		_ = json.NewEncoder(conn).Encode(ErrorResponse(request.RequestID, "protocol_mismatch", "unsupported protocol version", map[string]any{"supported": api.ProtocolVersion}))
		return
	}
	if request.Method == "events.subscribe" {
		s.serveEvents(conn, peer, request)
		return
	}
	_ = json.NewEncoder(conn).Encode(s.Handler.Handle(peer, request))
}

func (s *Server) serveEvents(conn *net.UnixConn, peer Peer, request Request) {
	events, cancel, rpcErr := s.Handler.Subscribe(peer)
	if rpcErr != nil {
		_ = json.NewEncoder(conn).Encode(ErrorResponse(request.RequestID, rpcErr.Code, rpcErr.Message, rpcErr.Details))
		return
	}
	defer cancel()
	_ = conn.SetDeadline(time.Time{})
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(SuccessResponse(request.RequestID, map[string]any{"subscribed": true})); err != nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := encoder.Encode(EventEnvelope{ProtocolVersion: api.ProtocolVersion, Event: event}); err != nil {
				return
			}
		case <-ticker.C:
			if err := encoder.Encode(EventEnvelope{ProtocolVersion: api.ProtocolVersion, Event: api.Event{Type: "heartbeat"}}); err != nil {
				return
			}
		}
	}
}

func (s *Server) prepareSocketDirectory() error {
	mode := os.FileMode(0o700)
	if s.Scope == ScopeSystem {
		mode = 0o755
	}
	if err := os.MkdirAll(filepath.Dir(s.SocketPath), mode); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	return os.Chmod(filepath.Dir(s.SocketPath), mode)
}

func (s *Server) setSocketPermissions() error {
	if s.Scope == ScopeUser {
		return os.Chmod(s.SocketPath, 0o600)
	}
	group, err := user.LookupGroup("admin")
	if err != nil {
		return fmt.Errorf("lookup admin group: %w", err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return fmt.Errorf("parse admin group id: %w", err)
	}
	if err := os.Chown(s.SocketPath, 0, gid); err != nil {
		return fmt.Errorf("set socket group: %w", err)
	}
	return os.Chmod(s.SocketPath, 0o660)
}

func SuccessResponse(requestID string, value any) Response {
	data, err := json.Marshal(value)
	if err != nil {
		return ErrorResponse(requestID, "internal", err.Error(), nil)
	}
	return Response{ProtocolVersion: api.ProtocolVersion, RequestID: requestID, OK: true, Result: data}
}

func ErrorResponse(requestID, code, message string, details map[string]any) Response {
	return Response{ProtocolVersion: api.ProtocolVersion, RequestID: requestID, OK: false, Error: &RPCError{Code: code, Message: message, Details: details}}
}

type Client struct {
	SocketPath string
	counter    atomic.Uint64
}

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	requestID := fmt.Sprintf("go-%d", c.counter.Add(1))
	request := Request{ProtocolVersion: api.ProtocolVersion, RequestID: requestID, Method: method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request.Params = data
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return fmt.Errorf("connect daemon: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(8 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return err
	}
	if !response.OK {
		return response.Error
	}
	if result != nil && len(response.Result) > 0 {
		return json.Unmarshal(response.Result, result)
	}
	return nil
}

type EventHub struct {
	mu          sync.Mutex
	nextID      uint64
	sequence    uint64
	subscribers map[uint64]chan api.Event
}

func NewEventHub() *EventHub {
	return &EventHub{subscribers: make(map[uint64]chan api.Event)}
}

func (h *EventHub) Publish(eventType, processID string, revision uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sequence++
	event := api.Event{Sequence: h.sequence, Type: eventType, ProcessID: processID, Revision: revision}
	for _, subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (h *EventHub) Subscribe() (<-chan api.Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	channel := make(chan api.Event, 64)
	h.subscribers[id] = channel
	var once sync.Once
	return channel, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.subscribers, id)
			close(channel)
		})
	}
}
