package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Participant struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Product       string `json:"product"`
	OfficialURL   string `json:"official_url"`
	Version       string `json:"version"`
	Configuration string `json:"configuration"`
	TokenHash     string `json:"token_hash,omitempty"`
}
type Config struct {
	Session      string        `json:"session"`
	Socket       string        `json:"socket"`
	Participants []Participant `json:"participants"`
	AdminHash    string        `json:"admin_hash"`
	Objective    string        `json:"objective,omitempty"`
	RepoPath     string        `json:"repo_path,omitempty"`
	Prepared     bool          `json:"prepared,omitempty"`
}
type Request struct {
	ProtocolVersion string       `json:"protocol_version"`
	Session         string       `json:"session_id"`
	RequestID       string       `json:"request_id"`
	Key             string       `json:"idempotency_key,omitempty"`
	Op              string       `json:"op"`
	To              string       `json:"to,omitempty"`
	Body            string       `json:"body,omitempty"`
	MessageID       string       `json:"message_id,omitempty"`
	TimeoutMS       int          `json:"timeout_ms,omitempty"`
	Activity        string       `json:"activity,omitempty"`
	ExpectedVersion int          `json:"expected_version,omitempty"`
	AgentID         string       `json:"agent_id,omitempty"`
	Participant     *Participant `json:"participant,omitempty"`
	After           int          `json:"after,omitempty"`
	Limit           int          `json:"limit,omitempty"`
	ActorFilter     string       `json:"actor_filter,omitempty"`
	TypeFilter      string       `json:"type_filter,omitempty"`
	Task            *Task        `json:"task,omitempty"`
	TaskID          string       `json:"task_id,omitempty"`
	ParentID        string       `json:"parent_id,omitempty"`
	Title           string       `json:"title,omitempty"`
	Acceptance      string       `json:"acceptance,omitempty"`
	Generation      int          `json:"generation,omitempty"`
	Reason          string       `json:"reason,omitempty"`
	State           string       `json:"state,omitempty"`
	Artifact        *Artifact    `json:"artifact,omitempty"`
	ArtifactID      string       `json:"artifact_id,omitempty"`
	Verdict         string       `json:"verdict,omitempty"`
	ProposalID      string       `json:"proposal_id,omitempty"`
	CandidateID     string       `json:"candidate_id,omitempty"`
	Choice          string       `json:"choice,omitempty"`
	TaskVersion     int          `json:"task_version,omitempty"`
	LeadershipTerm  int          `json:"leadership_term,omitempty"`
	ProposerID      string       `json:"proposer_id,omitempty"`
	Confirmed       bool         `json:"confirmed,omitempty"`
	ResultSHA       string       `json:"result_sha,omitempty"`
	Workspace       *Workspace   `json:"workspace,omitempty"`
	WorkspaceID     string       `json:"workspace_id,omitempty"`
	IntegrationOp   string       `json:"integration_op,omitempty"`
}
type Message struct {
	ID         string   `json:"id"`
	Sender     string   `json:"sender"`
	Recipients []string `json:"recipients"`
	Body       string   `json:"body"`
	Seq        int      `json:"seq"`
}
type Task struct {
	ID           string `json:"id"`
	ParentID     string `json:"parent_id,omitempty"`
	Title        string `json:"title"`
	Acceptance   string `json:"acceptance"`
	State        string `json:"state"`
	Version      int    `json:"version"`
	BlockedFrom  string `json:"blocked_from,omitempty"`
	AssignmentID string `json:"assignment_id,omitempty"`
	Assignee     string `json:"assignee,omitempty"`
	Generation   int    `json:"generation,omitempty"`
}
type Artifact struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	Author      string `json:"author"`
	Kind        string `json:"kind"`
	Reference   string `json:"reference"`
	Digest      string `json:"digest"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	TaskVersion int    `json:"task_version"`
}
type Review struct {
	ID         string `json:"id"`
	ArtifactID string `json:"artifact_id"`
	Reviewer   string `json:"reviewer"`
	Verdict    string `json:"verdict"`
	Reason     string `json:"reason"`
}
type Workspace struct {
	ID      string `json:"id"`
	Owner   string `json:"owner"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	BaseSHA string `json:"base_sha"`
	Status  string `json:"status"`
}
type Leadership struct {
	Coordinator       string    `json:"coordinator"`
	Term              int       `json:"term"`
	ProposalID        string    `json:"proposal_id,omitempty"`
	Candidate         string    `json:"candidate,omitempty"`
	Electorate        []string  `json:"electorate,omitempty"`
	VotesFor          int       `json:"votes_for,omitempty"`
	VotesAgainst      int       `json:"votes_against,omitempty"`
	Required          int       `json:"required,omitempty"`
	Deadline          time.Time `json:"deadline,omitempty"`
	IntegrationOwner  string    `json:"integration_owner,omitempty"`
	IntegrationTerm   int       `json:"integration_term,omitempty"`
	IntegrationStatus string    `json:"integration_status,omitempty"`
	IntegrationNote   string    `json:"integration_note,omitempty"`
}
type Event struct {
	EventID       string    `json:"event_id,omitempty"`
	SchemaVersion int       `json:"schema_version,omitempty"`
	Seq           int       `json:"seq"`
	At            time.Time `json:"at"`
	Actor         string    `json:"actor"`
	Type          string    `json:"type"`
	RequestID     string    `json:"request_id,omitempty"`
	Data          any       `json:"data,omitempty"`
}
type Presence struct {
	LastCall   time.Time `json:"last_agent_seen"`
	Activity   string    `json:"activity"`
	LeaseUntil time.Time `json:"lease_until,omitempty"`
	Health     string    `json:"health,omitempty"`
}
type Response struct {
	Error        string              `json:"error,omitempty"`
	Session      string              `json:"session_id,omitempty"`
	Status       string              `json:"status,omitempty"`
	Seq          int                 `json:"seq,omitempty"`
	MessageID    string              `json:"message_id,omitempty"`
	Messages     []Message           `json:"messages,omitempty"`
	Participants []Participant       `json:"participants,omitempty"`
	Presence     map[string]Presence `json:"presence,omitempty"`
	Events       []Event             `json:"events,omitempty"`
	Version      int                 `json:"version,omitempty"`
	ActorID      string              `json:"actor_id,omitempty"`
	Objective    string              `json:"objective,omitempty"`
	RepoPath     string              `json:"repo_path,omitempty"`
	NextCursor   int                 `json:"next_cursor,omitempty"`
	HasMore      bool                `json:"has_more,omitempty"`
	Tasks        []Task              `json:"tasks,omitempty"`
	Task         *Task               `json:"task,omitempty"`
	Artifact     *Artifact           `json:"artifact,omitempty"`
	Review       *Review             `json:"review,omitempty"`
	Leadership   *Leadership         `json:"leadership,omitempty"`
	Workspaces   []Workspace         `json:"workspaces,omitempty"`
}

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func Hash(s string) string    { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func ValidHash(s string) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == 32 }
func Validate(c Config) error {
	if c.Session == "" || c.Socket == "" || !ValidHash(c.AdminHash) {
		return errors.New("session, socket and admin credential required")
	}
	if !c.Prepared && len(c.Participants) < 3 {
		return errors.New("active sessions require at least three participants")
	}
	names := map[string]bool{}
	ids := map[string]bool{"human": true, "system": true, "*": true}
	hashes := map[string]bool{c.AdminHash: true}
	for _, p := range c.Participants {
		if p.ID == "" || p.Name == "" || p.Product == "" || p.Version == "" || p.OfficialURL == "" || p.Configuration == "" || !ValidHash(p.TokenHash) || names[p.Name] || ids[p.ID] || hashes[p.TokenHash] {
			return errors.New("incomplete or duplicate participant")
		}
		names[p.Name] = true
		ids[p.ID] = true
		hashes[p.TokenHash] = true
	}
	return nil
}
