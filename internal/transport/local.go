package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"agents-chat/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ClientConfig struct {
	Session         string `json:"session_id"`
	Socket          string `json:"socket"`
	AgentID         string `json:"agent_id"`
	Token           string `json:"token"`
	ProtocolVersion string `json:"protocol_version,omitempty"`
}
type Client struct {
	Config ClientConfig
	HTTP   *http.Client
}

func NewClient(c ClientConfig) *Client {
	return &Client{c, &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
		},
	}}}
}
func (c *Client) Call(ctx context.Context, r domain.Request) (domain.Response, error) {
	r.Session = c.Config.Session
	r.ProtocolVersion = c.Config.ProtocolVersion
	if r.ProtocolVersion == "" {
		r.ProtocolVersion = "0"
	}
	r.RequestID = domain.ID()
	data, err := json.Marshal(r)
	if err != nil {
		return domain.Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://local/call", bytes.NewReader(data))
	if err != nil {
		return domain.Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Config.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return domain.Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return domain.Response{}, fmt.Errorf("daemon HTTP status %d", resp.StatusCode)
	}
	var out domain.Response
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

type Service interface {
	Call(context.Context, string, domain.Request) domain.Response
}

func Handler(b Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/call" {
			http.NotFound(w, r)
			return
		}
		body := http.MaxBytesReader(w, r.Body, 65536)
		dec := json.NewDecoder(body)
		dec.DisallowUnknownFields()
		var req domain.Request
		if dec.Decode(&req) != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			http.Error(w, "trailing JSON", 400)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		out := b.Call(r.Context(), token, req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}

// ListenRecovered must be called only while holding the database writer lock.
// It removes only a refused Unix socket, never a file or a responding listener.
func ListenRecovered(socket string) (net.Listener, error) {
	info, err := os.Lstat(socket)
	if err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("existing socket path is not a Unix socket")
		}
		conn, dialErr := net.DialTimeout("unix", socket, time.Second)
		if dialErr == nil {
			conn.Close()
			return nil, errors.New("socket is already serving")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, syscall.ENOENT) {
			return nil, dialErr
		}
		if err = os.Remove(socket); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return Listen(socket)
}

// Listen refuses existing sockets rather than unlinking another daemon's socket.
// A crashed daemon's socket must be inspected and removed explicitly by the human.
func Listen(socket string) (net.Listener, error) {
	if !filepath.IsAbs(socket) {
		return nil, errors.New("socket path must be absolute")
	}
	info, err := os.Lstat(filepath.Dir(socket))
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("socket directory must be a real private directory (0700)")
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		return nil, errors.New("socket path exists or cannot be inspected; refusing to replace it")
	}
	l, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(socket, 0600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

type Empty struct{}
type Wait struct {
	TimeoutMS int `json:"timeout_ms,omitempty" jsonschema:"Wait duration in milliseconds, 1 to 25000; default 25000"`
	After     int `json:"after,omitempty" jsonschema:"Pagination cursor only; omit to redeliver all pending messages"`
	Limit     int `json:"limit,omitempty" jsonschema:"Maximum page size, 1 to 500; default 100"`
}
type Page struct {
	After int `json:"after,omitempty"`
	Limit int `json:"limit,omitempty"`
}
type Timeline struct {
	After int    `json:"after,omitempty"`
	Limit int    `json:"limit,omitempty"`
	Actor string `json:"actor_filter,omitempty"`
	Type  string `json:"type_filter,omitempty"`
}
type TaskInput struct {
	Key            string `json:"idempotency_key"`
	LeadershipTerm int    `json:"leadership_term,omitempty"`
	TaskID         string `json:"task_id,omitempty"`
	ParentID       string `json:"parent_id,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	Generation     int    `json:"generation,omitempty"`
	TaskVersion    int    `json:"task_version,omitempty"`
	Title          string `json:"title,omitempty"`
	Acceptance     string `json:"acceptance,omitempty"`
	State          string `json:"state,omitempty"`
	Reason         string `json:"reason,omitempty"`
}
type ArtifactInput struct {
	Key         string `json:"idempotency_key"`
	TaskID      string `json:"task_id"`
	Generation  int    `json:"generation"`
	TaskVersion int    `json:"task_version"`
	Kind        string `json:"kind"`
	Reference   string `json:"reference"`
	Digest      string `json:"digest"`
	CommitSHA   string `json:"commit_sha,omitempty"`
}
type ReviewInput struct {
	Key         string `json:"idempotency_key"`
	TaskID      string `json:"task_id"`
	ArtifactID  string `json:"artifact_id"`
	TaskVersion int    `json:"task_version"`
	Verdict     string `json:"verdict"`
	Reason      string `json:"reason"`
}
type LeadershipInput struct {
	Key            string `json:"idempotency_key,omitempty"`
	ProposalID     string `json:"proposal_id,omitempty"`
	CandidateID    string `json:"candidate_id,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Choice         string `json:"choice,omitempty"`
	Confirmed      bool   `json:"confirmed,omitempty"`
	LeadershipTerm int    `json:"leadership_term,omitempty"`
}
type Send struct {
	Key  string `json:"idempotency_key" jsonschema:"Unique logical operation key; reuse unchanged on retry"`
	To   string `json:"to" jsonschema:"Recipient UUID, human, or * for all participants"`
	Body string `json:"body" jsonschema:"Message content, at most 16384 bytes"`
}
type Ack struct {
	Key       string `json:"idempotency_key"`
	MessageID string `json:"message_id"`
}
type Activity struct {
	Key      string `json:"idempotency_key"`
	Activity string `json:"activity" jsonschema:"available, working, or waiting"`
}

func addTool[I any](s *mcp.Server, c *Client, name, description string, request func(I) domain.Request) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, any, error) {
		r := request(in)
		r.Op = name
		out, err := c.Call(ctx, r)
		if err != nil {
			return nil, nil, err
		}
		if out.Error != "" {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: out.Error}}}, nil, nil
		}
		return nil, out, nil
	})
}
func MCP(c *Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "agents-chat", Version: "0.2.0"}, nil)
	for _, name := range []string{"get_session_state", "list_agents", "reconnect_agent"} {
		addTool(s, c, name, "Inspect this collaboration session. Inbox reads never acknowledge messages.", func(Empty) domain.Request { return domain.Request{} })
	}
	addTool(s, c, "check_inbox", "Read pending messages without acknowledging. Cursor paginates; omit it on the next inbox pass.", func(v Page) domain.Request { return domain.Request{After: v.After, Limit: v.Limit} })
	addTool(s, c, "timeline", "Inspect session events without acknowledging messages or refreshing peers' activity.", func(v Timeline) domain.Request {
		return domain.Request{After: v.After, Limit: v.Limit, ActorFilter: v.Actor, TypeFilter: v.Type}
	})
	addTool(s, c, "wait_for_messages", "Wait for pending messages. Empty timeout means continue the collaboration loop; closed means stop.", func(v Wait) domain.Request {
		return domain.Request{TimeoutMS: v.TimeoutMS, After: v.After, Limit: v.Limit}
	})
	addTool(s, c, "send_message", "Send content to a session participant. Author is derived from bridge credentials.", func(v Send) domain.Request { return domain.Request{Key: v.Key, To: v.To, Body: v.Body} })
	addTool(s, c, "acknowledge_message", "Confirm receipt of your own message. This does not report completion of work.", func(v Ack) domain.Request { return domain.Request{Key: v.Key, MessageID: v.MessageID} })
	addTool(s, c, "set_activity", "Declare activity separately from actual tool-call evidence.", func(v Activity) domain.Request { return domain.Request{Key: v.Key, Activity: v.Activity} })
	addTool(s, c, "create_task", "Create a task or child task as current coordinator.", func(v TaskInput) domain.Request {
		return domain.Request{Key: v.Key, LeadershipTerm: v.LeadershipTerm, ParentID: v.ParentID, Title: v.Title, Acceptance: v.Acceptance}
	})
	addTool(s, c, "list_tasks", "List session tasks and assignment generations.", func(Page) domain.Request { return domain.Request{Op: "list_tasks"} })
	addTool(s, c, "get_task", "Inspect a task by ID.", func(v TaskInput) domain.Request { return domain.Request{TaskID: v.TaskID} })
	addTool(s, c, "assign_task", "Assign or reassign a proposed task as current coordinator.", func(v TaskInput) domain.Request {
		return domain.Request{Key: v.Key, LeadershipTerm: v.LeadershipTerm, TaskID: v.TaskID, AgentID: v.AgentID, TaskVersion: v.TaskVersion, Reason: v.Reason}
	})
	addTool(s, c, "accept_task", "Accept your current assignment generation.", func(v TaskInput) domain.Request {
		return domain.Request{Key: v.Key, TaskID: v.TaskID, Generation: v.Generation, TaskVersion: v.TaskVersion}
	})
	addTool(s, c, "update_task", "Report progress or a block; completion requires independent review.", func(v TaskInput) domain.Request {
		return domain.Request{Key: v.Key, LeadershipTerm: v.LeadershipTerm, TaskID: v.TaskID, Generation: v.Generation, TaskVersion: v.TaskVersion, State: v.State, Reason: v.Reason}
	})
	addTool(s, c, "publish_artifact", "Publish task evidence with an immutable reference and digest.", func(v ArtifactInput) domain.Request {
		return domain.Request{Key: v.Key, TaskID: v.TaskID, Generation: v.Generation, TaskVersion: v.TaskVersion, Artifact: &domain.Artifact{Kind: v.Kind, Reference: v.Reference, Digest: v.Digest, CommitSHA: v.CommitSHA}}
	})
	addTool(s, c, "request_review", "Request independent review of the latest unchanged artifact.", func(v TaskInput) domain.Request {
		return domain.Request{Key: v.Key, TaskID: v.TaskID, Generation: v.Generation, TaskVersion: v.TaskVersion}
	})
	addTool(s, c, "submit_review", "Approve an artifact or request changes as an independent reviewer.", func(v ReviewInput) domain.Request {
		return domain.Request{Key: v.Key, TaskID: v.TaskID, ArtifactID: v.ArtifactID, TaskVersion: v.TaskVersion, Verdict: v.Verdict, Reason: v.Reason}
	})
	addTool(s, c, "list_workspaces", "List registered worktrees and their recorded base revisions.", func(Empty) domain.Request { return domain.Request{} })
	addTool(s, c, "leadership_state", "Show the coordinator, term, and any frozen-electorate proposal.", func(Empty) domain.Request { return domain.Request{} })
	addTool(s, c, "propose_leadership", "Propose a reachable candidate; the electorate is frozen at proposal opening.", func(v LeadershipInput) domain.Request {
		return domain.Request{Key: v.Key, CandidateID: v.CandidateID, Reason: v.Reason}
	})
	addTool(s, c, "accept_leadership", "Accept or decline your nomination before voting.", func(v LeadershipInput) domain.Request {
		return domain.Request{Key: v.Key, ProposalID: v.ProposalID, Confirmed: v.Confirmed}
	})
	addTool(s, c, "vote_leadership", "Cast one vote in a proposal that the candidate accepted.", func(v LeadershipInput) domain.Request {
		return domain.Request{Key: v.Key, ProposalID: v.ProposalID, Choice: v.Choice}
	})
	addTool(s, c, "finalize_leadership", "Expire an unanswered proposal after its 120 second deadline.", func(v LeadershipInput) domain.Request { return domain.Request{Key: v.Key, ProposalID: v.ProposalID} })
	return s
}
