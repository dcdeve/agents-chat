package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agents-chat/internal/application"
	"agents-chat/internal/domain"
	"agents-chat/internal/transport"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func read(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func write(path string, v any) error {
	data, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(append(data, '\n')); e != nil {
		f.Close()
		return e
	}
	return f.Close()
}
func token() (string, error) {
	b := make([]byte, 32)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func initialize(dir, participants, objective, repo string, prepared bool) error {
	var ps []domain.Participant
	if participants != "" {
		if err := read(participants, &ps); err != nil {
			return err
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	c := domain.Config{Session: domain.ID(), Socket: filepath.Join(abs, "daemon.sock"), Participants: ps, Objective: objective, RepoPath: repo, Prepared: prepared}
	admin, err := token()
	if err != nil {
		return err
	}
	c.AdminHash = domain.Hash(admin)
	clients := []transport.ClientConfig{{Session: c.Session, Socket: c.Socket, AgentID: "human", Token: admin, ProtocolVersion: "1"}}
	for i := range c.Participants {
		p := &c.Participants[i]
		p.ID = domain.ID()
		t, err := token()
		if err != nil {
			return err
		}
		p.TokenHash = domain.Hash(t)
		clients = append(clients, transport.ClientConfig{Session: c.Session, Socket: c.Socket, AgentID: p.ID, Token: t, ProtocolVersion: "1"})
	}
	if err := domain.Validate(c); err != nil {
		return err
	}
	if len(c.Socket) > 100 {
		return errors.New("use a shorter absolute data directory for Unix socket portability")
	}
	if err := os.Mkdir(abs, 0700); err != nil {
		return fmt.Errorf("use a new data directory: %w", err)
	}
	if err := write(filepath.Join(abs, "server.json"), c); err != nil {
		return err
	}
	for i, client := range clients {
		name := "admin.json"
		if i > 0 {
			name = fmt.Sprintf("agent-%d.json", i)
		}
		if err := write(filepath.Join(abs, name), client); err != nil {
			return err
		}
	}
	fmt.Println("Prepared persistent session in", abs)
	for i, p := range c.Participants {
		fmt.Printf("agent-%d.json: %s (%s)\n", i+1, p.Name, p.ID)
	}
	return nil
}
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: collab init|serve|mcp|call|session|agent|timeline|export (see README.md)")
	}
	group, action := "", ""
	if args[0] == "session" || args[0] == "agent" || args[0] == "workspace" {
		if len(args) < 2 {
			return errors.New("session/agent subcommand required")
		}
		group, action = args[0], args[1]
		args = append([]string{group}, args[2:]...)
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := f.String("dir", "", "private experiment directory")
	participants := f.String("participants", "", "participant metadata JSON for init")
	config := f.String("config", "", "client credential file")
	op := f.String("op", "get_session_state", "operation for call")
	input := f.String("json", "{}", "operation parameters as JSON")
	objective := f.String("objective", "", "session objective")
	repo := f.String("repo", "", "repository path")
	output := f.String("output", "", "credential file for agent register (retained for retries)")
	owner := f.String("owner", "", "participant UUID for a prepared worktree")
	integration := f.Bool("integration", false, "prepare the coordinator integration worktree")
	base := f.String("base", "", "base commit for the new worktree (default: repository HEAD)")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if group == "session" && action == "create" {
		return initialize(*dir, *participants, *objective, *repo, true)
	}
	switch args[0] {
	case "init":
		return initialize(*dir, *participants, *objective, *repo, false)
	case "serve":
		if *dir == "" {
			return errors.New("--dir required")
		}
		var c domain.Config
		if err := read(filepath.Join(*dir, "server.json"), &c); err != nil {
			return err
		}
		if err := domain.Validate(c); err != nil {
			return err
		}
		dbPath := filepath.Join(*dir, "collab.sqlite")
		if _, e := os.Stat(filepath.Join(*dir, "started")); e == nil {
			if _, e = os.Stat(dbPath); os.IsNotExist(e) {
				return errors.New("legacy volatile run has no recoverable database; initialize a new session")
			}
		}
		b, err := application.Open(dbPath, c)
		if err != nil {
			return err
		}
		defer b.Close()
		listener, err := transport.ListenRecovered(c.Socket)
		if err != nil {
			return err
		}
		defer listener.Close()
		server := &http.Server{Handler: transport.Handler(b), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
		stopped := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				server.Close()
			case <-stopped:
			}
		}()
		maintenanceDone := make(chan struct{})
		go func() {
			defer close(maintenanceDone)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-stopped:
					return
				case <-ticker.C:
					if e := b.Expire(ctx); e != nil && ctx.Err() == nil {
						fmt.Fprintln(os.Stderr, "presence maintenance:", e)
					}
				}
			}
		}()
		// Wait for maintenance to finish before closing SQLite.
		defer func() { close(stopped); <-maintenanceDone }()
		fmt.Fprintln(os.Stderr, "Persistent daemon listening:", c.Socket)
		err = server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case "mcp", "call", "export", "timeline", "session", "agent", "workspace":
		var c transport.ClientConfig
		if err := read(*config, &c); err != nil {
			return err
		}
		client := transport.NewClient(c)
		defer client.HTTP.CloseIdleConnections()
		if args[0] == "mcp" {
			if c.AgentID == "human" {
				return errors.New("MCP requires participant credentials")
			}
			return transport.MCP(client).Run(ctx, &mcp.StdioTransport{})
		}
		if group == "workspace" {
			var r domain.Request
			if err := json.Unmarshal([]byte(*input), &r); err != nil {
				return err
			}
			return runWorkspace(ctx, action, client, r, *owner, *integration, *base)
		}
		var r domain.Request
		if err := json.Unmarshal([]byte(*input), &r); err != nil {
			return err
		}
		r.Op = *op
		if group != "" {
			operations := map[string]string{"session:start": "start_session", "session:show": "get_session_state", "session:close": "close_session", "agent:list": "list_agents", "agent:register": "register_agent", "agent:revoke": "revoke_agent", "agent:reconnect": "reconnect_agent"}
			var ok bool
			r.Op, ok = operations[group+":"+action]
			if !ok {
				return errors.New("unknown subcommand")
			}
		}
		if args[0] == "timeline" {
			r.Op = "timeline"
		}
		if args[0] == "export" {
			r.Op = "export"
		}
		if r.Op == "register_agent" {
			if *output == "" || r.Participant == nil {
				return errors.New("agent register requires --output and participant metadata in --json")
			}
			var enrollment transport.ClientConfig
			if err := read(*output, &enrollment); os.IsNotExist(err) {
				secret, e := token()
				if e != nil {
					return e
				}
				enrollment = transport.ClientConfig{Session: c.Session, Socket: c.Socket, AgentID: domain.ID(), Token: secret, ProtocolVersion: "1"}
				if e = write(*output, enrollment); e != nil {
					return e
				}
			} else if err != nil {
				return err
			}
			if enrollment.Session != c.Session || enrollment.Socket != c.Socket || enrollment.AgentID == "human" {
				return errors.New("credential output belongs to a different enrollment")
			}
			r.Participant.ID = enrollment.AgentID
			r.Participant.TokenHash = domain.Hash(enrollment.Token)
		}
		out, err := client.Call(ctx, r)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			return err
		}
		if out.Error != "" {
			return errors.New(out.Error)
		}
		return nil
	default:
		return errors.New("unknown command: " + args[0])
	}
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
