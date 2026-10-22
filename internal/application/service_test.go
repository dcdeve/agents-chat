package application

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agents-chat/internal/domain"
)

func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
func fixture(t *testing.T) (*Service, domain.Config, string) {
	t.Helper()
	c := domain.Config{Session: "s", Socket: "unused", AdminHash: domain.Hash("admin"), Objective: "recover", RepoPath: "/repo"}
	for _, id := range []string{"a", "b", "c"} {
		c.Participants = append(c.Participants, domain.Participant{ID: id, Name: id, Product: "test", Version: "1", OfficialURL: "https://example.org", Configuration: "test", TokenHash: domain.Hash(id)})
	}
	path := filepath.Join(privateDir(t), "state.sqlite")
	s, err := Open(path, c)
	if err != nil {
		t.Fatal(err)
	}
	return s, c, path
}
func call(s *Service, actor, op string, r domain.Request) domain.Response {
	r.ProtocolVersion = "1"
	r.Session = "s"
	r.RequestID = domain.ID()
	r.Op = op
	return s.Call(context.Background(), actor, r)
}
func ok(t *testing.T, r domain.Response) domain.Response {
	t.Helper()
	if r.Error != "" {
		t.Fatal(r)
	}
	return r
}

func TestRestartLostResponseAndACK(t *testing.T) {
	s, c, path := fixture(t)
	request := domain.Request{Key: "one", To: "b", Body: "durable"}
	original := ok(t, call(s, "a", "send_message", request))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	retry := ok(t, call(s, "a", "send_message", request))
	if encoded(retry) != encoded(original) {
		t.Fatal("response changed after lost response/restart")
	}
	for i := 0; i < 2; i++ {
		inbox := ok(t, call(s, "b", "check_inbox", domain.Request{}))
		if len(inbox.Messages) != 1 || inbox.Messages[0].ID != original.MessageID {
			t.Fatal(inbox)
		}
	}
	ok(t, call(s, "b", "acknowledge_message", domain.Request{Key: "ack", MessageID: original.MessageID}))
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(ok(t, call(s, "b", "check_inbox", domain.Request{})).Messages) != 0 {
		t.Fatal("ACK lost")
	}
	ok(t, call(s, "b", "acknowledge_message", domain.Request{Key: "ack", MessageID: original.MessageID}))
	events := ok(t, call(s, "admin", "export", domain.Request{})).Events
	count := 0
	for i, e := range events {
		if e.Seq != i+1 {
			t.Fatal("sequence gap")
		}
		if e.Type == "message" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("logical duplicate", count)
	}
	request.Body = "other"
	if call(s, "a", "send_message", request).Error != "idempotency_conflict" {
		t.Fatal("key conflict lost on restart")
	}
}

func TestAtomicFailureAndAppendOnlyEvents(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	// Fail after message/event insertion but before receipts and idempotency.
	_, err := s.store.DB.Exec("CREATE TRIGGER inject_failure BEFORE INSERT ON message_receipts BEGIN SELECT RAISE(ABORT,'injected write failure'); END;")
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err = s.store.DB.QueryRow("SELECT seq FROM sessions").Scan(&before); err != nil {
		t.Fatal(err)
	}
	r := domain.Request{Key: "write", To: "b", Body: "recoverable"}
	if call(s, "a", "send_message", r).Error != "storage_error" {
		t.Fatal("failed transaction reported success")
	}
	var after, count int
	if err = s.store.DB.QueryRow("SELECT seq FROM sessions").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("event sequence committed without message")
	}
	for _, table := range []string{"messages", "message_receipts", "idempotency"} {
		if err = s.store.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial write", table, count, err)
		}
	}
	if _, err = s.store.DB.Exec("DROP TRIGGER inject_failure"); err != nil {
		t.Fatal(err)
	}
	ok(t, call(s, "a", "send_message", r))
	if _, err = s.store.DB.Exec("DELETE FROM events"); err == nil {
		t.Fatal("append-only event deletion allowed")
	}
	if _, err = s.store.DB.Exec("UPDATE events SET type='changed'"); err == nil {
		t.Fatal("append-only event update allowed")
	}
}

func TestConcurrentReplayAndWait(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	received := make(chan domain.Response, 1)
	go func() { received <- call(s, "b", "wait_for_messages", domain.Request{TimeoutMS: 2000}) }()
	var wg sync.WaitGroup
	responses := make(chan domain.Response, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- call(s, "a", "send_message", domain.Request{Key: "same", To: "*", Body: "concurrent"})
		}()
	}
	wg.Wait()
	close(responses)
	id := ""
	for r := range responses {
		ok(t, r)
		if id == "" {
			id = r.MessageID
		}
		if id != r.MessageID {
			t.Fatal("concurrent duplicate")
		}
	}
	select {
	case r := <-received:
		ok(t, r)
		if len(r.Messages) != 1 {
			t.Fatal(r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait held transaction or lost notification")
	}
}

func TestAuthorizationRevocationAndExclusiveWriter(t *testing.T) {
	s, c, path := fixture(t)
	if second, err := Open(path, c); err == nil {
		second.Close()
		t.Fatal("second writer admitted")
	}
	if call(s, "unknown", "get_session_state", domain.Request{}).Error != "unauthorized" {
		t.Fatal("unknown identity")
	}
	r := domain.Request{ProtocolVersion: "1", Session: "another", RequestID: "x", Op: "get_session_state"}
	if s.Call(context.Background(), "a", r).Error != "unauthorized" {
		t.Fatal("cross-session credential")
	}
	if call(s, "a", "close_session", domain.Request{Key: "x", ExpectedVersion: 1}).Error != "unauthorized" {
		t.Fatal("agent gained admin privileges")
	}
	sent := ok(t, call(s, "a", "send_message", domain.Request{Key: "send", To: "b", Body: "private inbox"}))
	if call(s, "c", "acknowledge_message", domain.Request{Key: "ack", MessageID: sent.MessageID}).Error != "not_found" {
		t.Fatal("foreign ACK accepted")
	}
	if len(ok(t, call(s, "c", "check_inbox", domain.Request{})).Messages) != 0 {
		t.Fatal("foreign inbox visible")
	}
	waiter := make(chan domain.Response, 1)
	go func() { waiter <- call(s, "c", "wait_for_messages", domain.Request{}) }()
	ok(t, call(s, "admin", "revoke_agent", domain.Request{Key: "revoke", AgentID: "c", ExpectedVersion: 1}))
	select {
	case r := <-waiter:
		if r.Error != "unauthorized" {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked wait not cancelled")
	}
	s.Close()
	s, err := Open(path, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if call(s, "c", "reconnect_agent", domain.Request{}).Error != "unauthorized" {
		t.Fatal("bootstrap restored revoked identity")
	}
	state := ok(t, call(s, "admin", "get_session_state", domain.Request{}))
	if len(state.Participants) != 2 {
		t.Fatal(state)
	}
	text := encoded(ok(t, call(s, "admin", "export", domain.Request{})))
	if strings.Contains(text, domain.Hash("c")) || strings.Contains(text, "token_hash") {
		t.Fatal("credential leak")
	}
}

func TestPreparedLifecycleAndVersionConflicts(t *testing.T) {
	c := domain.Config{Session: "s", Socket: "unused", AdminHash: domain.Hash("admin"), Prepared: true}
	s, err := Open(filepath.Join(privateDir(t), "state.sqlite"), c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if call(s, "admin", "start_session", domain.Request{Key: "early", ExpectedVersion: 1}).Error != "insufficient_members" {
		t.Fatal("started with zero members")
	}
	if call(s, "admin", "send_message", domain.Request{Key: "early-send", To: "human", Body: "work"}).Error != "session_not_active" {
		t.Fatal("work before start")
	}
	for i, id := range []string{"a", "b", "c"} {
		p := domain.Participant{ID: id, Name: id, Product: "test", Version: "1", OfficialURL: "https://example.org", Configuration: "test", TokenHash: domain.Hash(id)}
		ok(t, call(s, "admin", "register_agent", domain.Request{Key: "register-" + id, ExpectedVersion: i + 1, Participant: &p}))
	}
	if call(s, "admin", "start_session", domain.Request{Key: "stale", ExpectedVersion: 1}).Error != "version_conflict" {
		t.Fatal("stale version allowed")
	}
	ok(t, call(s, "admin", "start_session", domain.Request{Key: "start", ExpectedVersion: 4}))
	if call(s, "admin", "register_agent", domain.Request{Key: "frozen", ExpectedVersion: 5}).Error != "membership_frozen" {
		t.Fatal("membership not frozen")
	}
	waiter := make(chan domain.Response, 1)
	go func() { waiter <- call(s, "a", "wait_for_messages", domain.Request{}) }()
	ok(t, call(s, "admin", "close_session", domain.Request{Key: "close", ExpectedVersion: 5}))
	select {
	case r := <-waiter:
		if r.Status != "closed" {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not notify")
	}
	if call(s, "a", "send_message", domain.Request{Key: "late", To: "b", Body: "late"}).Error != "session_closed" {
		t.Fatal("closed mutation")
	}
}

func TestPaginationDoesNotACKAndPresenceIsNotConnectivity(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	now := time.Now()
	s.now = func() time.Time { return now }
	for _, key := range []string{"1", "2", "3"} {
		ok(t, call(s, "a", "send_message", domain.Request{Key: key, To: "b", Body: key}))
	}
	first := ok(t, call(s, "b", "check_inbox", domain.Request{Limit: 2}))
	if len(first.Messages) != 2 || !first.HasMore {
		t.Fatal(first)
	}
	second := ok(t, call(s, "b", "check_inbox", domain.Request{Limit: 2, After: first.NextCursor}))
	if len(second.Messages) != 1 || second.HasMore {
		t.Fatal(second)
	}
	if len(ok(t, call(s, "b", "check_inbox", domain.Request{})).Messages) != 3 {
		t.Fatal("cursor acknowledged messages")
	}
	original := ok(t, call(s, "admin", "get_session_state", domain.Request{})).Presence["b"].LastCall
	now = now.Add(91 * time.Second)
	if err := s.Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := ok(t, call(s, "admin", "get_session_state", domain.Request{}))
	if !state.Presence["b"].LastCall.Equal(original) || state.Presence["b"].Health != "unreachable" {
		t.Fatal("observer renewed presence", state)
	}
	events := ok(t, call(s, "admin", "timeline", domain.Request{TypeFilter: "agent_unreachable"})).Events
	if len(events) != 2 {
		t.Fatal("expiry must be recorded once per previously active agent", len(events))
	}
	ok(t, call(s, "b", "reconnect_agent", domain.Request{}))
	state = ok(t, call(s, "admin", "get_session_state", domain.Request{}))
	if state.Presence["b"].Health != "recent" {
		t.Fatal(state)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := domain.Request{ProtocolVersion: "1", Session: "s", RequestID: "cancel", Op: "wait_for_messages"}
	if got := s.Call(ctx, "c", r); got.Error != "cancelled" {
		t.Fatal(got)
	}
	page := ok(t, call(s, "admin", "timeline", domain.Request{Limit: 2}))
	if len(page.Events) != 2 || !page.HasMore {
		t.Fatal(page)
	}
	raw, _ := json.Marshal(page)
	if len(raw) == 0 {
		t.Fatal("empty serialization")
	}
}
