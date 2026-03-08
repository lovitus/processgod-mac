package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/lovitus/processgod-mac/internal/guardian"
)

type Request struct {
	Action string `json:"action"`
	ID     string `json:"id,omitempty"`
	Lines  int    `json:"lines,omitempty"`
}

type Response struct {
	OK      bool              `json:"ok"`
	Message string            `json:"message,omitempty"`
	Error   string            `json:"error,omitempty"`
	Logs    string            `json:"logs,omitempty"`
	Status  []guardian.Status `json:"status,omitempty"`
}

type Handler interface {
	Reload() error
	Statuses() []guardian.Status
	Logs(id string, lines int) (string, error)
	Shutdown() error
}

type Server struct {
	controlAddr string
	handler     Handler
}

func NewServer(controlAddr string, handler Handler) *Server {
	return &Server{controlAddr: controlAddr, handler: handler}
}

func (s *Server) Run(stop <-chan struct{}) error {
	ln, err := net.Listen("tcp", s.controlAddr)
	if err != nil {
		return fmt.Errorf("listen control address: %w", err)
	}
	defer func() {
		ln.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		<-stop
		_ = ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if isClosedErr(err) {
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

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	reader := bufio.NewReader(conn)
	decoder := json.NewDecoder(reader)
	var req Request
	if err := decoder.Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: fmt.Sprintf("decode request: %v", err)})
		return
	}

	resp := s.dispatch(req)
	_ = json.NewEncoder(conn).Encode(resp)
}

func (s *Server) dispatch(req Request) Response {
	switch req.Action {
	case "ping":
		return Response{OK: true, Message: "pong"}
	case "reload":
		if err := s.handler.Reload(); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "reloaded"}
	case "status":
		return Response{OK: true, Status: s.handler.Statuses()}
	case "logs":
		logs, err := s.handler.Logs(req.ID, req.Lines)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Logs: logs}
	case "shutdown":
		if err := s.handler.Shutdown(); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "shutdown requested"}
	default:
		return Response{OK: false, Error: "unknown action"}
	}
}

func Send(controlAddr string, req Request) (Response, error) {
	conn, err := net.DialTimeout("tcp", controlAddr, 3*time.Second)
	if err != nil {
		return Response{}, fmt.Errorf("dial daemon: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("encode request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
}

func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == "use of closed network connection"
}
