// Package application implements the persistent session and mailbox operations.
package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"agents-chat/internal/domain"
	"agents-chat/internal/storage"
)

type Service struct {
	mu      sync.Mutex
	store   *storage.Store
	session string
	changed chan struct{}
	now     func() time.Time
}
type fault string

func (f fault) Error() string { return string(f) }
func encoded(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func stamp(t time.Time) string   { return t.UTC().Format(time.RFC3339Nano) }
func instant(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }

func Open(path string, c domain.Config) (*Service, error) {
	if err := domain.Validate(c); err != nil {
		return nil, err
	}
	store, err := storage.Open(path)
	if err != nil {
		return nil, err
	}
	s := &Service{store: store, session: c.Session, changed: make(chan struct{}), now: time.Now}
	if err = s.seed(c); err != nil {
		store.Close()
		return nil, err
	}
	return s, nil
}
func (s *Service) Close() error { return s.store.Close() }
func (s *Service) seed(c domain.Config) error {
	tx, err := s.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		var id string
		if err = tx.QueryRow("SELECT id FROM sessions").Scan(&id); err != nil {
			return err
		}
		if id != c.Session {
			return errors.New("database belongs to another session")
		}
		// Existing credentials and membership are authoritative; never restore
		// revoked credentials or overwrite current state from the bootstrap file.
		return nil
	}
	status := "active"
	if c.Prepared {
		status = "prepared"
	}
	if _, err = tx.Exec("INSERT INTO sessions(id,objective,repo_path,status,version) VALUES(?,?,?,?,1)", c.Session, c.Objective, c.RepoPath, status); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO credentials(session_id,actor_id,token_hash) VALUES(?,?,?)", c.Session, "human", c.AdminHash); err != nil {
		return err
	}
	// This identity cannot authenticate; it attributes server observations without
	// impersonating the human administrator.
	if _, err = tx.Exec("INSERT INTO credentials(session_id,actor_id,token_hash,revoked) VALUES(?,?,?,1)", c.Session, "system", domain.Hash(domain.ID())); err != nil {
		return err
	}
	if _, err = s.event(tx, "human", "session_created", "bootstrap", map[string]any{"objective": c.Objective, "repo_path": c.RepoPath, "status": status}); err != nil {
		return err
	}
	for _, p := range c.Participants {
		if err = s.register(tx, p, "bootstrap"); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Service) register(tx *sql.Tx, p domain.Participant, request string) error {
	hash := p.TokenHash
	p.TokenHash = ""
	if _, err := tx.Exec("INSERT INTO agents(session_id,id,name,metadata_json) VALUES(?,?,?,?)", s.session, p.ID, p.Name, encoded(p)); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO credentials(session_id,actor_id,token_hash) VALUES(?,?,?)", s.session, p.ID, hash); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE sessions SET coordinator_id=? WHERE id=? AND coordinator_id=''", p.ID, s.session); err != nil {
		return err
	}
	_, err := s.event(tx, "human", "agent_registered", request, p)
	return err
}
func (s *Service) event(tx *sql.Tx, actor, kind, request string, data any) (int, error) {
	var seq int
	if err := tx.QueryRow("UPDATE sessions SET seq=seq+1 WHERE id=? RETURNING seq", s.session).Scan(&seq); err != nil {
		return 0, err
	}
	_, err := tx.Exec("INSERT INTO events(session_id,seq,event_id,type,actor_id,request_id,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?)", s.session, seq, domain.ID(), kind, actor, request, encoded(data), stamp(s.now()))
	return seq, err
}
func (s *Service) auth(tx *sql.Tx, token string) (string, error) {
	var actor string
	err := tx.QueryRow("SELECT actor_id FROM credentials WHERE session_id=? AND token_hash=? AND revoked=0", s.session, domain.Hash(token)).Scan(&actor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fault("unauthorized")
	}
	return actor, err
}
func isMutation(op string) bool {
	switch op {
	case "send_message", "acknowledge_message", "set_activity", "start_session", "close_session", "register_agent", "revoke_agent", "create_task", "assign_task", "accept_task", "update_task", "publish_artifact", "request_review", "submit_review", "propose_leadership", "accept_leadership", "vote_leadership", "finalize_leadership", "force_leadership", "register_workspace", "begin_integration", "finish_integration", "recover_integration":
		return true
	}
	return false
}
func supported(op string) bool {
	if isMutation(op) {
		return true
	}
	switch op {
	case "get_session_state", "list_agents", "reconnect_agent", "check_inbox", "wait_for_messages", "timeline", "export", "list_tasks", "get_task", "list_workspaces", "leadership_state":
		return true
	}
	return false
}
func adminOnly(op string) bool {
	switch op {
	case "start_session", "close_session", "register_agent", "revoke_agent", "force_leadership", "register_workspace", "recover_integration":
		return true
	}
	return false
}
func (s *Service) Call(ctx context.Context, token string, r domain.Request) domain.Response {
	if r.Session != s.session {
		return domain.Response{Error: "unauthorized"}
	}
	if (r.ProtocolVersion != "0" && r.ProtocolVersion != "1") || r.RequestID == "" || len(r.RequestID) > 128 || len(r.Key) > 128 || r.After < 0 || r.Limit < 0 || r.Limit > 500 {
		return domain.Response{Error: "invalid_request"}
	}
	if !supported(r.Op) {
		return domain.Response{Error: "not_found"}
	}
	if r.TimeoutMS < 0 || r.TimeoutMS > 25000 {
		return domain.Response{Error: "invalid_timeout"}
	}
	if isMutation(r.Op) && r.Key == "" {
		return domain.Response{Error: "invalid_request"}
	}
	timeout := time.Duration(r.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 25 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	record := true
	terminal := ""
	for {
		s.mu.Lock()
		out, err := s.transact(ctx, token, r, record, terminal)
		changed := s.changed
		if err == nil && isMutation(r.Op) {
			switch r.Op {
			case "send_message", "close_session", "start_session", "revoke_agent", "create_task", "assign_task", "accept_task", "update_task", "publish_artifact", "request_review", "submit_review", "propose_leadership", "accept_leadership", "vote_leadership", "finalize_leadership", "force_leadership", "register_workspace", "begin_integration", "finish_integration", "recover_integration":
				close(s.changed)
				s.changed = make(chan struct{})
			}
		}
		s.mu.Unlock()
		if err != nil {
			var f fault
			if errors.As(err, &f) {
				return domain.Response{Error: string(f)}
			}
			if ctx.Err() != nil {
				return domain.Response{Error: "cancelled"}
			}
			log.Printf("operation %s failed: %v", r.Op, err)
			return domain.Response{Error: "storage_error"}
		}
		if r.Op != "wait_for_messages" || len(out.Messages) > 0 || out.Status == "closed" || terminal != "" {
			return out
		}
		record = false
		// No transaction is open here. The subscription was captured under the
		// mutex protecting both the inbox read and commits that notify waiters.
		select {
		case <-ctx.Done():
			return domain.Response{Error: "cancelled"}
		case <-timer.C:
			terminal = "wait_timeout"
		case <-changed:
		}
	}
}
func (s *Service) transact(ctx context.Context, token string, r domain.Request, record bool, terminal string) (domain.Response, error) {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Response{}, err
	}
	defer tx.Rollback()
	actor, err := s.auth(tx, token)
	if err != nil {
		return domain.Response{}, err
	}
	fail := func(code string) (domain.Response, error) { return domain.Response{}, fault(code) }
	if adminOnly(r.Op) && actor != "human" {
		return fail("unauthorized")
	}
	out := domain.Response{Session: s.session, ActorID: actor}
	if err = tx.QueryRow("SELECT status,version,objective,repo_path,seq FROM sessions WHERE id=?", s.session).Scan(&out.Status, &out.Version, &out.Objective, &out.RepoPath, &out.Seq); err != nil {
		return out, err
	}
	if record {
		if actor != "human" {
			now := s.now()
			if _, err = tx.Exec("UPDATE agents SET last_agent_seen=?,lease_until=?,health='recent' WHERE session_id=? AND id=?", stamp(now), stamp(now.Add(90*time.Second)), s.session, actor); err != nil {
				return out, err
			}
		}
		if _, err = s.event(tx, actor, "call", r.RequestID, map[string]string{"op": r.Op}); err != nil {
			return out, err
		}
	}
	canonical := r
	canonical.RequestID = ""
	canonical.ProtocolVersion = "1"
	hash := domain.Hash(encoded(canonical))
	if isMutation(r.Op) {
		var priorHash, result string
		err = tx.QueryRow("SELECT request_hash,response_json FROM idempotency WHERE session_id=? AND actor_id=? AND key=?", s.session, actor, r.Key).Scan(&priorHash, &result)
		if err == nil {
			if priorHash != hash {
				return fail("idempotency_conflict")
			}
			if err = json.Unmarshal([]byte(result), &out); err != nil {
				return out, err
			}
			return out, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if out.Status == "closed" {
			return fail("session_closed")
		}
		if adminOnly(r.Op) && r.Op != "force_leadership" && r.ExpectedVersion != out.Version {
			return fail("version_conflict")
		}
	}
	switch r.Op {
	case "get_session_state", "list_agents", "reconnect_agent":
		out.Participants, out.Presence, err = s.agents(tx)
		if err == nil && r.Op == "get_session_state" {
			var leadership domain.Leadership
			leadership, err = s.leadership(tx)
			if err == nil {
				out.Leadership = &leadership
				err = tx.QueryRow("SELECT coordinator_id,leadership_term FROM sessions WHERE id=?", s.session).Scan(&out.Leadership.Coordinator, &out.Leadership.Term)
			}
		}
	case "create_task", "assign_task", "accept_task", "update_task", "publish_artifact", "request_review", "submit_review", "list_tasks", "get_task":
		out, err = s.taskOperation(tx, actor, r, out)
	case "propose_leadership", "accept_leadership", "vote_leadership", "finalize_leadership", "force_leadership", "leadership_state":
		out, err = s.leadershipOperation(tx, actor, r, out)
	case "register_workspace", "list_workspaces", "begin_integration", "finish_integration", "recover_integration":
		out, err = s.workspaceOperation(tx, actor, r, out)
	case "start_session":
		if out.Status != "prepared" {
			return fail("invalid_transition")
		}
		var count int
		err = tx.QueryRow("SELECT count(*) FROM agents a JOIN credentials c ON c.session_id=a.session_id AND c.actor_id=a.id WHERE a.session_id=? AND c.revoked=0", s.session).Scan(&count)
		if err != nil {
			return out, err
		}
		if count < 3 {
			return fail("insufficient_members")
		}
		out.Status = "active"
		out.Version++
		_, err = tx.Exec("UPDATE sessions SET status=?,version=? WHERE id=?", out.Status, out.Version, s.session)
		if err == nil {
			_, err = s.event(tx, actor, "session_started", r.RequestID, nil)
		}
	case "close_session":
		out.Status = "closed"
		out.Version++
		_, err = tx.Exec("UPDATE sessions SET status=?,version=? WHERE id=?", out.Status, out.Version, s.session)
		if err == nil {
			_, err = s.event(tx, actor, "session_closed", r.RequestID, nil)
		}
	case "register_agent":
		if out.Status != "prepared" {
			return fail("membership_frozen")
		}
		if r.Participant == nil {
			return fail("invalid_request")
		}
		p := *r.Participant
		if e := domain.Validate(domain.Config{Session: s.session, Socket: "unused", AdminHash: domain.Hash("validation"), Prepared: true, Participants: []domain.Participant{p}}); e != nil {
			return fail("invalid_participant")
		}
		var count int
		if err = tx.QueryRow("SELECT count(*) FROM agents WHERE session_id=? AND (id=? OR name=?)", s.session, p.ID, p.Name).Scan(&count); err != nil {
			return out, err
		}
		if count > 0 {
			return fail("already_exists")
		}
		if err = tx.QueryRow("SELECT count(*) FROM credentials WHERE session_id=? AND token_hash=?", s.session, p.TokenHash).Scan(&count); err != nil {
			return out, err
		}
		if count > 0 {
			return fail("already_exists")
		}
		err = s.register(tx, p, r.RequestID)
		if err == nil {
			out.Version++
			_, err = tx.Exec("UPDATE sessions SET version=? WHERE id=?", out.Version, s.session)
		}
		out.ActorID = p.ID
	case "revoke_agent":
		if r.AgentID == "human" || r.AgentID == "system" {
			return fail("unauthorized")
		}
		res, e := tx.Exec("UPDATE credentials SET revoked=1 WHERE session_id=? AND actor_id=? AND revoked=0", s.session, r.AgentID)
		if e != nil {
			return out, e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return out, e
		}
		if n != 1 {
			return fail("not_found")
		}
		if _, err = tx.Exec("UPDATE agents SET health='unreachable',lease_until='' WHERE session_id=? AND id=?", s.session, r.AgentID); err == nil {
			_, err = s.event(tx, actor, "agent_revoked", r.RequestID, map[string]string{"agent_id": r.AgentID})
		}
		if err == nil {
			out.Version++
			_, err = tx.Exec("UPDATE sessions SET version=? WHERE id=?", out.Version, s.session)
		}
	case "set_activity":
		if actor == "human" {
			return fail("unauthorized")
		}
		if r.Activity != "available" && r.Activity != "working" && r.Activity != "waiting" {
			return fail("invalid_activity")
		}
		_, err = tx.Exec("UPDATE agents SET activity=? WHERE session_id=? AND id=?", r.Activity, s.session, actor)
		if err == nil {
			_, err = s.event(tx, actor, "activity", r.RequestID, r.Activity)
		}
	case "send_message":
		if out.Status != "active" {
			return fail("session_not_active")
		}
		if len(r.Body) == 0 || len(r.Body) > 16384 {
			return fail("invalid_body")
		}
		var recipients []string
		rows, e := tx.Query("SELECT actor_id FROM credentials WHERE session_id=? AND revoked=0 AND (actor_id=? OR (?='*' AND actor_id!='human')) ORDER BY actor_id", s.session, r.To, r.To)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, e
			}
			recipients = append(recipients, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if len(recipients) == 0 {
			return fail("not_found")
		}
		m := domain.Message{ID: domain.ID(), Sender: actor, Body: r.Body, Recipients: recipients}
		if err = tx.QueryRow("SELECT seq+1 FROM sessions WHERE id=?", s.session).Scan(&m.Seq); err != nil {
			return out, err
		}
		if _, err = s.event(tx, actor, "message", r.RequestID, m); err != nil {
			return out, err
		}
		if _, err = tx.Exec("INSERT INTO messages(session_id,id,sender_id,body,seq) VALUES(?,?,?,?,?)", s.session, m.ID, actor, m.Body, m.Seq); err != nil {
			return out, err
		}
		for _, id := range recipients {
			if _, err = tx.Exec("INSERT INTO message_receipts(session_id,message_id,recipient_id) VALUES(?,?,?)", s.session, m.ID, id); err != nil {
				return out, err
			}
		}
		out.MessageID = m.ID
	case "acknowledge_message":
		var ack sql.NullString
		err = tx.QueryRow("SELECT acked_at FROM message_receipts WHERE session_id=? AND message_id=? AND recipient_id=?", s.session, r.MessageID, actor).Scan(&ack)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		if !ack.Valid {
			_, err = tx.Exec("UPDATE message_receipts SET acked_at=? WHERE session_id=? AND message_id=? AND recipient_id=?", stamp(s.now()), s.session, r.MessageID, actor)
			if err == nil {
				_, err = s.event(tx, actor, "ack", r.RequestID, r.MessageID)
			}
		}
	case "check_inbox", "wait_for_messages":
		out.Messages, out.HasMore, err = s.inbox(tx, actor, r.After, r.Limit)
		if err != nil {
			return out, err
		}
		if len(out.Messages) > 0 {
			ids := []string{}
			for _, m := range out.Messages {
				ids = append(ids, m.ID)
				if _, err = tx.Exec("UPDATE message_receipts SET first_delivered_at=COALESCE(first_delivered_at,?) WHERE session_id=? AND message_id=? AND recipient_id=?", stamp(s.now()), s.session, m.ID, actor); err != nil {
					return out, err
				}
			}
			out.NextCursor = out.Messages[len(out.Messages)-1].Seq
			_, err = s.event(tx, actor, "delivery", r.RequestID, ids)
		} else if terminal != "" {
			_, err = s.event(tx, actor, terminal, r.RequestID, nil)
		}
	case "timeline", "export":
		out.Events, out.HasMore, err = s.events(tx, r)
		if len(out.Events) > 0 {
			out.NextCursor = out.Events[len(out.Events)-1].Seq
		}
	}
	if err != nil {
		return out, err
	}
	if err = tx.QueryRow("SELECT seq FROM sessions WHERE id=?", s.session).Scan(&out.Seq); err != nil {
		return out, err
	}
	if isMutation(r.Op) {
		if _, err = tx.Exec("INSERT INTO idempotency(session_id,actor_id,key,request_hash,response_json) VALUES(?,?,?,?,?)", s.session, actor, r.Key, hash, encoded(out)); err != nil {
			return out, err
		}
	}
	return out, tx.Commit()
}

func (s *Service) agents(tx *sql.Tx) ([]domain.Participant, map[string]domain.Presence, error) {
	rows, err := tx.Query("SELECT a.metadata_json,a.activity,a.last_agent_seen,a.lease_until,a.health FROM agents a JOIN credentials c ON c.session_id=a.session_id AND c.actor_id=a.id WHERE a.session_id=? AND c.revoked=0 ORDER BY a.name", s.session)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ps []domain.Participant
	presence := map[string]domain.Presence{}
	for rows.Next() {
		var data, last, lease string
		var p domain.Participant
		var seen domain.Presence
		if err = rows.Scan(&data, &seen.Activity, &last, &lease, &seen.Health); err != nil {
			return nil, nil, err
		}
		if err = json.Unmarshal([]byte(data), &p); err != nil {
			return nil, nil, err
		}
		seen.LastCall = instant(last)
		seen.LeaseUntil = instant(lease)
		if !seen.LeaseUntil.After(s.now()) {
			seen.Health = "unreachable"
		}
		ps = append(ps, p)
		presence[p.ID] = seen
	}
	return ps, presence, rows.Err()
}
func pageSize(n int) int {
	if n == 0 {
		return 100
	}
	return n
}
func (s *Service) inbox(tx *sql.Tx, actor string, after, limit int) ([]domain.Message, bool, error) {
	limit = pageSize(limit)
	rows, err := tx.Query("SELECT m.id,m.sender_id,m.body,m.seq FROM messages m JOIN message_receipts r ON r.session_id=m.session_id AND r.message_id=m.id WHERE m.session_id=? AND r.recipient_id=? AND r.acked_at IS NULL AND m.seq>? ORDER BY m.seq LIMIT ?", s.session, actor, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	var ms []domain.Message
	for rows.Next() {
		var m domain.Message
		if err = rows.Scan(&m.ID, &m.Sender, &m.Body, &m.Seq); err != nil {
			rows.Close()
			return nil, false, err
		}
		ms = append(ms, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	more := len(ms) > limit
	if more {
		ms = ms[:limit]
	}
	for i := range ms {
		rows, err = tx.Query("SELECT recipient_id FROM message_receipts WHERE session_id=? AND message_id=? ORDER BY recipient_id", s.session, ms[i].ID)
		if err != nil {
			return nil, false, err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, false, err
			}
			ms[i].Recipients = append(ms[i].Recipients, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, false, err
		}
	}
	return ms, more, nil
}
func (s *Service) events(tx *sql.Tx, r domain.Request) ([]domain.Event, bool, error) {
	limit := pageSize(r.Limit)
	if r.Op == "export" {
		limit = -2
	} // Human/local export is a full point-in-time snapshot.
	rows, err := tx.Query("SELECT seq,type,actor_id,request_id,payload_json,created_at,event_id,schema_version FROM events WHERE session_id=? AND seq>? AND (?='' OR actor_id=?) AND (?='' OR type=?) ORDER BY seq LIMIT ?", s.session, r.After, r.ActorFilter, r.ActorFilter, r.TypeFilter, r.TypeFilter, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var data, at string
		if err = rows.Scan(&e.Seq, &e.Type, &e.Actor, &e.RequestID, &data, &at, &e.EventID, &e.SchemaVersion); err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal([]byte(data), &e.Data); err != nil {
			return nil, false, err
		}
		e.At = instant(at)
		events = append(events, e)
	}
	more := limit > 0 && len(events) > limit
	if more {
		events = events[:limit]
	}
	return events, more, rows.Err()
}

// Expire records suspicion once, without changing assignments or claiming that
// the terminal has exited. The daemon calls this independently of MCP bridges.
func (s *Service) Expire(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT id,lease_until FROM agents WHERE session_id=? AND health='recent'", s.session)
	if err != nil {
		return err
	}
	var expired []string
	for rows.Next() {
		var id, lease string
		if err = rows.Scan(&id, &lease); err != nil {
			rows.Close()
			return err
		}
		if !instant(lease).After(s.now()) {
			expired = append(expired, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range expired {
		if _, err = tx.Exec("UPDATE agents SET health='unreachable' WHERE session_id=? AND id=?", s.session, id); err != nil {
			return err
		}
		if _, err = s.event(tx, "system", "agent_unreachable", "lease-expiration", map[string]string{"agent_id": id}); err != nil {
			return err
		}
	}
	if len(expired) == 0 {
		return nil
	}
	return tx.Commit()
}

func (s *Service) String() string { return fmt.Sprintf("session %s", s.session) }
