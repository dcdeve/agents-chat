package transport

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agents-chat/internal/probe"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPThroughUnixSocket(t *testing.T) {
	// Short path also works on macOS (Unix socket paths have a small fixed limit).
	dir, err := os.MkdirTemp("/tmp", "ac-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "d.sock")
	listener, err := Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := Listen(socket); err == nil {
		t.Fatal("second daemon replaced socket")
	}
	b := probe.New(probe.Config{Session: "s", AdminHash: probe.Hash("admin"), Participants: []probe.Participant{{ID: "a", TokenHash: probe.Hash("a")}, {ID: "b", TokenHash: probe.Hash("b")}}})
	server := &http.Server{Handler: Handler(b)}
	defer server.Close()
	go server.Serve(listener)
	a := NewClient(ClientConfig{Session: "s", Socket: socket, Token: "a"})
	defer a.HTTP.CloseIdleConnections()
	c := NewClient(ClientConfig{Session: "s", Socket: socket, Token: "b"})
	defer c.HTTP.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := MCP(c).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "simulated-client", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 24 {
		t.Fatalf("tools: %v %v", list, err)
	}
	wait := make(chan *mcp.CallToolResult, 1)
	go func() {
		r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "wait_for_messages", Arguments: map[string]any{"timeout_ms": 2000}})
		if e != nil {
			wait <- nil
			return
		}
		wait <- r
	}()
	sent, err := a.Call(ctx, probe.Request{Op: "send_message", Key: "send", To: "b", Body: "hello"})
	if err != nil || sent.Error != "" {
		t.Fatal(sent, err)
	}
	result := <-wait
	if result == nil || result.IsError {
		t.Fatal(result)
	}
	inbox, err := c.Call(ctx, probe.Request{Op: "check_inbox"})
	if err != nil || len(inbox.Messages) != 1 {
		t.Fatal(inbox, err)
	}
	ack, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "acknowledge_message", Arguments: map[string]any{"idempotency_key": "ack", "message_id": sent.MessageID}})
	if err != nil || ack.IsError {
		t.Fatal(ack, err)
	}
	inbox, err = c.Call(ctx, probe.Request{Op: "check_inbox"})
	if err != nil || len(inbox.Messages) != 0 {
		t.Fatal(inbox, err)
	}
	bad, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "send_message", Arguments: map[string]any{"idempotency_key": "bad", "to": "missing", "body": "x"}})
	if err != nil || !bad.IsError {
		t.Fatal("domain error must be MCP tool error", bad, err)
	}
}
