package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lovitus/processgod-mac/internal/api"
)

type testHandler struct {
	hub  *EventHub
	peer Peer
}

func (h *testHandler) Handle(peer Peer, request Request) Response {
	h.peer = peer
	return SuccessResponse(request.RequestID, map[string]string{"method": request.Method})
}

func (h *testHandler) Subscribe(peer Peer) (<-chan api.Event, func(), *RPCError) {
	h.peer = peer
	events, cancel := h.hub.Subscribe()
	return events, cancel, nil
}

func TestUnixRPCAndEventSubscription(t *testing.T) {
	runDir := filepath.Join("/tmp", fmt.Sprintf("pg-ipc-%d", os.Getpid()))
	socket := filepath.Join(runDir, "control.sock")
	_ = os.RemoveAll(runDir)
	t.Cleanup(func() { _ = os.RemoveAll(runDir) })
	stop := make(chan struct{})
	handler := &testHandler{hub: NewEventHub()}
	server := &Server{SocketPath: socket, Scope: ScopeUser, Handler: handler}
	done := make(chan error, 1)
	go func() { done <- server.Run(stop) }()
	waitForSocket(t, socket)
	t.Cleanup(func() {
		close(stop)
		<-done
	})

	client := &Client{SocketPath: socket}
	var result map[string]string
	if err := client.Call(context.Background(), "system.hello", nil, &result); err != nil {
		t.Fatalf("RPC call: %v", err)
	}
	if result["method"] != "system.hello" || handler.peer.UID != uint32(os.Getuid()) {
		t.Fatalf("unexpected result/peer: result=%v peer=%+v", result, handler.peer)
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("subscribe dial: %v", err)
	}
	defer conn.Close()
	request := Request{ProtocolVersion: api.ProtocolVersion, RequestID: "subscribe-1", Method: "events.subscribe"}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		t.Fatalf("subscribe request: %v", err)
	}
	reader := bufio.NewReader(conn)
	var response Response
	if err := json.NewDecoder(reader).Decode(&response); err != nil || !response.OK {
		t.Fatalf("subscribe response: response=%+v err=%v", response, err)
	}
	handler.hub.Publish("status.changed", "job", 3)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var envelope EventEnvelope
	if err := json.NewDecoder(reader).Decode(&envelope); err != nil {
		t.Fatalf("event decode: %v", err)
	}
	if envelope.Event.Type != "status.changed" || envelope.Event.ProcessID != "job" || envelope.Event.Revision != 3 {
		t.Fatalf("unexpected event: %+v", envelope.Event)
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket was not created: %s", path)
}
