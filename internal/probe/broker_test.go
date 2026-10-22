package probe

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func fixture() *Broker {
	return New(Config{Session: "s", AdminHash: Hash("admin"), Participants: []Participant{
		{ID: "a", Name: "a", TokenHash: Hash("a")}, {ID: "b", Name: "b", TokenHash: Hash("b")}, {ID: "c", Name: "c", TokenHash: Hash("c")},
	}})
}
func call(b *Broker, actor, op string, r Request) Response {
	r.ProtocolVersion = "0"
	r.RequestID = ID()
	r.Session = "s"
	r.Op = op
	return b.Call(context.Background(), actor, r)
}
func TestDeliveryRetryAndAuthorization(t *testing.T) {
	b := fixture()
	r := Request{Key: "one", To: "b", Body: "work"}
	first := call(b, "a", "send_message", r)
	if first.Error != "" || first.MessageID == "" {
		t.Fatal(first)
	}
	again := call(b, "a", "send_message", r)
	if first.MessageID != again.MessageID || first.Seq != again.Seq {
		t.Fatal("retry changed result")
	}
	r.Body = "changed"
	if call(b, "a", "send_message", r).Error != "idempotency_conflict" {
		t.Fatal("key collision accepted")
	}
	for i := 0; i < 2; i++ {
		if len(call(b, "b", "check_inbox", Request{}).Messages) != 1 {
			t.Fatal("read consumed message")
		}
	}
	if len(call(b, "c", "check_inbox", Request{}).Messages) != 0 {
		t.Fatal("wrong inbox")
	}
	if call(b, "c", "acknowledge_message", Request{Key: "ack", MessageID: first.MessageID}).Error != "unauthorized" {
		t.Fatal("foreign ACK")
	}
	for i := 0; i < 2; i++ {
		if got := call(b, "b", "acknowledge_message", Request{Key: "ack", MessageID: first.MessageID}); got.Error != "" {
			t.Fatal(got)
		}
	}
	if len(call(b, "b", "check_inbox", Request{}).Messages) != 0 {
		t.Fatal("ACK ignored")
	}
	if call(b, "a", "close_session", Request{Key: "close"}).Error != "unauthorized" {
		t.Fatal("agent closed session")
	}
	if call(b, "wrong", "list_agents", Request{}).Error != "unauthorized" {
		t.Fatal("unknown credential")
	}
	if b.Call(context.Background(), "a", Request{ProtocolVersion: "0", Session: "other", RequestID: "id", Op: "list_agents"}).Error != "unauthorized" {
		t.Fatal("cross-session access")
	}
	state := call(b, "a", "get_session_state", Request{})
	for _, p := range state.Participants {
		if p.TokenHash != "" {
			t.Fatal("credential leak")
		}
	}
}
func TestConcurrentSendIsExactlyOneLogicalMutation(t *testing.T) {
	b := fixture()
	var wg sync.WaitGroup
	ids := make(chan string, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := call(b, "a", "send_message", Request{Key: "same", To: "*", Body: "broadcast"})
			ids <- r.MessageID
		}()
	}
	wg.Wait()
	close(ids)
	expected := ""
	for id := range ids {
		if expected == "" {
			expected = id
		}
		if id != expected {
			t.Fatal("duplicate logical send")
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if len(call(b, id, "check_inbox", Request{}).Messages) != 1 {
			t.Fatal("broadcast recipients")
		}
	}
}
func TestWaitArrivalTimeoutCancellationAndClose(t *testing.T) {
	for i := 0; i < 50; i++ {
		b := fixture()
		done := make(chan Response, 1)
		go func() { done <- call(b, "b", "wait_for_messages", Request{TimeoutMS: 1000}) }()
		call(b, "a", "send_message", Request{Key: "send", To: "b", Body: "race"})
		select {
		case r := <-done:
			if len(r.Messages) != 1 {
				t.Fatal(r)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("lost wakeup")
		}
	}
	b := fixture()
	if r := call(b, "b", "wait_for_messages", Request{TimeoutMS: 2}); r.Error != "" || len(r.Messages) != 0 {
		t.Fatal(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := b.Call(ctx, "b", Request{ProtocolVersion: "0", Session: "s", RequestID: "cancel", Op: "wait_for_messages"})
	if r.Error != "cancelled" {
		t.Fatal(r)
	}
	done := make(chan Response, 1)
	go func() { done <- call(b, "b", "wait_for_messages", Request{}) }()
	call(b, "admin", "close_session", Request{Key: "close"})
	select {
	case r := <-done:
		if r.Status != "closed" {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not wake wait")
	}
	if call(b, "a", "send_message", Request{Key: "late", To: "b", Body: "late"}).Error != "session_closed" {
		t.Fatal("closed mutation")
	}
}
func TestEvidenceAndPresence(t *testing.T) {
	b := fixture()
	r := call(b, "a", "send_message", Request{Key: "send", To: "b", Body: "hello"})
	call(b, "b", "check_inbox", Request{})
	call(b, "admin", "export", Request{}) // must neither ACK nor renew agent activity
	state := call(b, "a", "get_session_state", Request{})
	before := state.Presence["b"].LastCall
	state = call(b, "a", "get_session_state", Request{})
	if !state.Presence["b"].LastCall.Equal(before) {
		t.Fatal("observer refreshed presence")
	}
	call(b, "b", "acknowledge_message", Request{Key: "ack", MessageID: r.MessageID})
	events := call(b, "admin", "export", Request{}).Events
	for i, e := range events {
		if e.Seq != i+1 {
			t.Fatal("event sequence")
		}
	}
	if _, err := json.Marshal(events); err != nil {
		t.Fatal(err)
	}
}
