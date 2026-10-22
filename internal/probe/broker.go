// Package probe implements the deliberately volatile phase-zero experiment.
// It is not the persistent application core planned for phase one.
package probe

import (
	"agents-chat/internal/domain"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Participant = domain.Participant
type Config = domain.Config
type Request = domain.Request
type Message = domain.Message
type Event = domain.Event
type Presence = domain.Presence
type Response = domain.Response
type replay struct {
	Hash   string
	Result Response
}
type Broker struct {
	mu       sync.Mutex
	config   Config
	closed   bool
	messages []Message
	ack      map[string]map[string]bool
	events   []Event
	presence map[string]Presence
	replays  map[string]replay
	changed  chan struct{}
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
func Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func New(c Config) *Broker {
	return &Broker{config: c, ack: map[string]map[string]bool{}, presence: map[string]Presence{}, replays: map[string]replay{}, changed: make(chan struct{})}
}
func (b *Broker) actor(token string) string {
	h := Hash(token)
	if h == b.config.AdminHash {
		return "human"
	}
	for _, p := range b.config.Participants {
		if p.TokenHash == h {
			return p.ID
		}
	}
	return ""
}
func (b *Broker) event(actor, kind, request string, data any) {
	b.events = append(b.events, Event{Seq: len(b.events) + 1, At: time.Now().UTC(), Actor: actor, Type: kind, RequestID: request, Data: data})
}
func (b *Broker) notify() { close(b.changed); b.changed = make(chan struct{}) }
func (b *Broker) status() string {
	if b.closed {
		return "closed"
	}
	return "active"
}
func (b *Broker) inbox(actor string) []Message {
	var out []Message
	for _, m := range b.messages {
		if receipt, ok := b.ack[m.ID][actor]; ok && !receipt {
			out = append(out, m)
		}
	}
	return out
}

// Call authenticates before recording actual agent activity. No bridge heartbeat
// renews presence. A wait registers its wake channel while holding the same lock
// used by send/close, so arrivals cannot fall between reading and subscribing.
func (b *Broker) Call(ctx context.Context, token string, r Request) Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	fail := func(s string) Response { return Response{Error: s} }
	actor := b.actor(token)
	if actor == "" || r.Session != b.config.Session {
		return fail("unauthorized")
	}
	if r.ProtocolVersion != "0" || r.RequestID == "" {
		return fail("invalid_request")
	}
	switch r.Op {
	case "get_session_state", "list_agents", "check_inbox", "wait_for_messages", "send_message", "acknowledge_message", "set_activity", "close_session", "export":
	default:
		return fail("not_found")
	}
	if (r.Op == "close_session" || r.Op == "export") && actor != "human" {
		return fail("unauthorized")
	}
	if r.TimeoutMS < 0 || r.TimeoutMS > 25000 {
		return fail("invalid_timeout")
	}
	if actor != "human" {
		p := b.presence[actor]
		p.LastCall = time.Now().UTC()
		b.presence[actor] = p
	}
	b.event(actor, "call", r.RequestID, map[string]any{"op": r.Op})
	mutation := r.Op == "send_message" || r.Op == "acknowledge_message" || r.Op == "close_session" || r.Op == "set_activity"
	key := actor + ":" + r.Key
	canonical := r
	canonical.RequestID = ""
	raw, _ := json.Marshal(canonical)
	hash := Hash(string(raw))
	if mutation {
		if r.Key == "" {
			return fail("invalid_request")
		}
		if old, ok := b.replays[key]; ok {
			if old.Hash != hash {
				return fail("idempotency_conflict")
			}
			return old.Result
		}
		if b.closed {
			return fail("session_closed")
		}
	}
	out := Response{Session: b.config.Session, Status: b.status()}
	switch r.Op {
	case "get_session_state", "list_agents":
		out.Participants = append([]Participant(nil), b.config.Participants...)
		for i := range out.Participants {
			out.Participants[i].TokenHash = ""
		}
		out.Presence = map[string]Presence{}
		for k, v := range b.presence {
			out.Presence[k] = v
		}
	case "export":
		out.Events = append([]Event(nil), b.events...)
	case "close_session":
		b.closed = true
		out.Status = "closed"
		b.event(actor, "session_closed", r.RequestID, nil)
		b.notify()
	case "set_activity":
		if actor == "human" {
			return fail("unauthorized")
		}
		if r.Activity != "available" && r.Activity != "working" && r.Activity != "waiting" {
			return fail("invalid_activity")
		}
		p := b.presence[actor]
		p.Activity = r.Activity
		b.presence[actor] = p
		b.event(actor, "activity", r.RequestID, r.Activity)
	case "send_message":
		if len(r.Body) == 0 || len(r.Body) > 16384 {
			return fail("invalid_body")
		}
		var recipients []string
		if r.To == "human" {
			recipients = append(recipients, "human")
		} else {
			for _, p := range b.config.Participants {
				if r.To == "*" || r.To == p.ID {
					recipients = append(recipients, p.ID)
				}
			}
		}
		if len(recipients) == 0 {
			return fail("not_found")
		}
		m := Message{ID: ID(), Sender: actor, Recipients: recipients, Body: r.Body, Seq: len(b.events) + 1}
		b.messages = append(b.messages, m)
		b.ack[m.ID] = map[string]bool{}
		for _, id := range recipients {
			b.ack[m.ID][id] = false
		}
		b.event(actor, "message", r.RequestID, m)
		out.MessageID = m.ID
		b.notify()
	case "acknowledge_message":
		receipts, ok := b.ack[r.MessageID]
		if !ok {
			return fail("not_found")
		}
		if _, ok := receipts[actor]; !ok {
			return fail("unauthorized")
		}
		if !receipts[actor] {
			receipts[actor] = true
			b.event(actor, "ack", r.RequestID, r.MessageID)
		}
	case "check_inbox", "wait_for_messages":
		timeout := time.Duration(r.TimeoutMS) * time.Millisecond
		if r.Op == "wait_for_messages" && timeout == 0 {
			timeout = 25 * time.Second
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		for {
			out.Messages = b.inbox(actor)
			if len(out.Messages) > 0 || b.closed || r.Op == "check_inbox" {
				break
			}
			changed := b.changed
			b.mu.Unlock()
			var stop string
			select {
			case <-ctx.Done():
				stop = "cancelled"
			case <-timer.C:
				stop = "timeout"
			case <-changed:
			}
			b.mu.Lock()
			if stop != "" {
				b.event(actor, "wait_"+stop, r.RequestID, nil)
				if stop == "cancelled" {
					return fail("cancelled")
				}
				out.Messages = b.inbox(actor)
				break
			}
		}
		out.Status = b.status()
		if len(out.Messages) > 0 {
			ids := []string{}
			for _, m := range out.Messages {
				ids = append(ids, m.ID)
			}
			b.event(actor, "delivery", r.RequestID, ids)
		}
	}
	out.Seq = len(b.events)
	if mutation {
		b.replays[key] = replay{hash, out}
	}
	return out
}

func Validate(c Config) error {
	if c.Session == "" || c.Socket == "" || len(c.AdminHash) != 64 || len(c.Participants) < 3 {
		return errors.New("session, socket, admin credential and at least three participants required")
	}
	names := map[string]bool{}
	ids := map[string]bool{"human": true, "*": true}
	hashes := map[string]bool{c.AdminHash: true}
	for _, p := range c.Participants {
		if p.ID == "" || p.Name == "" || p.Product == "" || p.Version == "" || p.OfficialURL == "" || p.Configuration == "" || len(p.TokenHash) != 64 || names[p.Name] || ids[p.ID] || hashes[p.TokenHash] {
			return errors.New("incomplete or duplicate participant")
		}
		names[p.Name] = true
		ids[p.ID] = true
		hashes[p.TokenHash] = true
	}
	return nil
}
