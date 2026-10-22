package application

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"agents-chat/internal/domain"
)

func member(tx *sql.Tx, session, actor string) error {
	var id string
	err := tx.QueryRow("SELECT id FROM agents WHERE session_id=? AND id=?", session, actor).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fault("unauthorized")
	}
	return err
}

func (s *Service) leadershipOperation(tx *sql.Tx, actor string, r domain.Request, out domain.Response) (domain.Response, error) {
	fail := func(code string) (domain.Response, error) { return domain.Response{}, fault(code) }
	var status, coordinator string
	var term int
	if err := tx.QueryRow("SELECT status,coordinator_id,leadership_term FROM sessions WHERE id=?", s.session).Scan(&status, &coordinator, &term); err != nil {
		return out, err
	}
	if r.Op != "leadership_state" && status != "active" {
		return fail("session_not_active")
	}
	if r.Op != "force_leadership" && r.Op != "leadership_state" {
		if err := member(tx, s.session, actor); err != nil {
			return out, err
		}
	}
	switch r.Op {
	case "leadership_state":
	case "propose_leadership":
		if r.CandidateID == "" || r.CandidateID == coordinator || r.Reason == "" {
			return fail("invalid_proposal")
		}
		var candidate string
		err := tx.QueryRow("SELECT a.id FROM agents a JOIN credentials c ON c.session_id=a.session_id AND c.actor_id=a.id WHERE a.session_id=? AND a.id=? AND c.revoked=0 AND a.lease_until>?", s.session, r.CandidateID, stamp(s.now())).Scan(&candidate)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("candidate_unavailable")
		}
		if err != nil {
			return out, err
		}
		rows, err := tx.Query("SELECT a.id FROM agents a JOIN credentials c ON c.session_id=a.session_id AND c.actor_id=a.id WHERE a.session_id=? AND c.revoked=0 AND a.lease_until>? ORDER BY a.id", s.session, stamp(s.now()))
		if err != nil {
			return out, err
		}
		var electorate []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return out, err
			}
			electorate = append(electorate, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if len(electorate) < 2 {
			return fail("insufficient_electorate")
		}
		var pending int
		if err = tx.QueryRow("SELECT count(*) FROM leadership_proposals WHERE session_id=? AND status='pending'", s.session).Scan(&pending); err != nil {
			return out, err
		}
		if pending > 0 {
			return fail("leadership_pending")
		}
		id := domain.ID()
		now := s.now()
		deadline := now.Add(120 * time.Second)
		_, err = tx.Exec("INSERT INTO leadership_proposals(session_id,id,proposer_id,candidate_id,electorate_json,reason,opened_at,deadline,status) VALUES(?,?,?,?,?,?,?,?,'pending')", s.session, id, actor, r.CandidateID, encoded(electorate), r.Reason, stamp(now), stamp(deadline))
		if err != nil {
			return out, err
		}
		if _, err = s.event(tx, actor, "leadership_proposed", r.RequestID, map[string]any{"id": id, "candidate": candidate, "electorate": electorate, "reason": r.Reason, "deadline": stamp(deadline)}); err != nil {
			return out, err
		}
		r.ProposalID = id
	case "accept_leadership":
		var candidate string
		var deadline string
		err := tx.QueryRow("SELECT candidate_id,deadline FROM leadership_proposals WHERE session_id=? AND id=? AND status='pending'", s.session, r.ProposalID).Scan(&candidate, &deadline)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		if actor != candidate {
			return fail("unauthorized")
		}
		if !instant(deadline).After(s.now()) {
			return fail("proposal_expired")
		}
		accepted := 1
		if !r.Confirmed {
			accepted = 0
		}
		if _, err = tx.Exec("UPDATE leadership_proposals SET candidate_accepted=? WHERE session_id=? AND id=?", accepted, s.session, r.ProposalID); err != nil {
			return out, err
		}
		if !r.Confirmed {
			_, err = tx.Exec("UPDATE leadership_proposals SET status='rejected' WHERE session_id=? AND id=?", s.session, r.ProposalID)
		}
		if err == nil {
			_, err = s.event(tx, actor, "leadership_candidate_response", r.RequestID, map[string]any{"proposal_id": r.ProposalID, "accepted": r.Confirmed})
		}
	case "vote_leadership":
		if r.Choice != "approve" && r.Choice != "reject" {
			return fail("invalid_vote")
		}
		var candidate, electorate, deadline string
		var accepted int
		err := tx.QueryRow("SELECT candidate_id,electorate_json,deadline,candidate_accepted FROM leadership_proposals WHERE session_id=? AND id=? AND status='pending'", s.session, r.ProposalID).Scan(&candidate, &electorate, &deadline, &accepted)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		if !instant(deadline).After(s.now()) {
			return fail("proposal_expired")
		}
		if accepted != 1 {
			return fail("candidate_not_accepted")
		}
		var voters []string
		if err = json.Unmarshal([]byte(electorate), &voters); err != nil {
			return out, err
		}
		eligible := false
		for _, id := range voters {
			if id == actor {
				eligible = true
				break
			}
		}
		if !eligible {
			return fail("not_in_electorate")
		}
		var previous int
		if err = tx.QueryRow("SELECT count(*) FROM votes WHERE session_id=? AND proposal_id=? AND agent_id=?", s.session, r.ProposalID, actor).Scan(&previous); err != nil {
			return out, err
		}
		if previous > 0 {
			return fail("already_voted")
		}
		seq, err := s.event(tx, actor, "leadership_vote", r.RequestID, map[string]string{"proposal_id": r.ProposalID, "choice": r.Choice})
		if err != nil {
			return out, err
		}
		if _, err = tx.Exec("INSERT INTO votes(session_id,proposal_id,agent_id,choice,seq) VALUES(?,?,?,?,?)", s.session, r.ProposalID, actor, r.Choice, seq); err != nil {
			return out, err
		}
		var yes, no int
		if err = tx.QueryRow("SELECT count(*) FILTER(WHERE choice='approve'),count(*) FILTER(WHERE choice='reject') FROM votes WHERE session_id=? AND proposal_id=?", s.session, r.ProposalID).Scan(&yes, &no); err != nil {
			return out, err
		}
		required := len(voters)/2 + 1
		if yes >= required {
			if _, err = tx.Exec("UPDATE leadership_proposals SET status='approved' WHERE session_id=? AND id=?", s.session, r.ProposalID); err != nil {
				return out, err
			}
			if _, err = tx.Exec("UPDATE sessions SET coordinator_id=?,leadership_term=leadership_term+1 WHERE id=?", candidate, s.session); err != nil {
				return out, err
			}
			if _, err = s.event(tx, actor, "leadership_changed", r.RequestID, map[string]any{"previous": coordinator, "coordinator": candidate, "term": term + 1, "proposal_id": r.ProposalID}); err != nil {
				return out, err
			}
		} else if no >= required {
			if _, err = tx.Exec("UPDATE leadership_proposals SET status='rejected' WHERE session_id=? AND id=?", s.session, r.ProposalID); err != nil {
				return out, err
			}
			if _, err = s.event(tx, actor, "leadership_rejected", r.RequestID, map[string]any{"proposal_id": r.ProposalID}); err != nil {
				return out, err
			}
		}
	case "finalize_leadership":
		var deadline string
		err := tx.QueryRow("SELECT deadline FROM leadership_proposals WHERE session_id=? AND id=? AND status='pending'", s.session, r.ProposalID).Scan(&deadline)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		if instant(deadline).After(s.now()) {
			return fail("vote_still_open")
		}
		if _, err = tx.Exec("UPDATE leadership_proposals SET status='expired' WHERE session_id=? AND id=?", s.session, r.ProposalID); err != nil {
			return out, err
		}
		_, err = s.event(tx, actor, "leadership_expired", r.RequestID, r.ProposalID)
	case "force_leadership":
		if actor != "human" {
			return fail("unauthorized")
		}
		if r.Reason == "" || r.ExpectedVersion != term {
			return fail("version_conflict")
		}
		if r.CandidateID == "" {
			return fail("invalid_candidate")
		}
		var found string
		err := tx.QueryRow("SELECT a.id FROM agents a JOIN credentials c ON c.session_id=a.session_id AND c.actor_id=a.id WHERE a.session_id=? AND a.id=? AND c.revoked=0", s.session, r.CandidateID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		_, _ = tx.Exec("UPDATE leadership_proposals SET status='rejected' WHERE session_id=? AND status='pending'", s.session)
		if _, err = tx.Exec("UPDATE sessions SET coordinator_id=?,leadership_term=leadership_term+1 WHERE id=?", found, s.session); err != nil {
			return out, err
		}
		_, err = s.event(tx, actor, "leadership_forced", r.RequestID, map[string]any{"previous": coordinator, "coordinator": found, "term": term + 1, "reason": r.Reason})
	}
	l, err := s.leadership(tx)
	if err != nil {
		return out, err
	}
	out.Leadership = &l
	if err = tx.QueryRow("SELECT coordinator_id,leadership_term FROM sessions WHERE id=?", s.session).Scan(&out.Leadership.Coordinator, &out.Leadership.Term); err != nil {
		return out, err
	}
	if err = tx.QueryRow("SELECT seq FROM sessions WHERE id=?", s.session).Scan(&out.Seq); err != nil {
		return out, err
	}
	if out.Leadership.ProposalID != "" && out.Leadership.ProposalID != r.ProposalID {
		out.Leadership.ProposalID = ""
	}
	return out, nil
}
