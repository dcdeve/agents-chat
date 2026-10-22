package application

import (
	"testing"
	"time"

	"agents-chat/internal/domain"
)

func TestFrozenElectorateAcceptanceMajorityAndStaleCoordinator(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	ok(t, call(s, "b", "get_session_state", domain.Request{}))
	ok(t, call(s, "c", "get_session_state", domain.Request{}))
	proposal := ok(t, call(s, "a", "propose_leadership", domain.Request{Key: "proposal", CandidateID: "b", Reason: "review quality"}))
	id := proposal.Leadership.ProposalID
	if proposal.Leadership.Required != 2 || len(proposal.Leadership.Electorate) != 3 {
		t.Fatal(proposal.Leadership)
	}
	if call(s, "b", "vote_leadership", domain.Request{Key: "premature", ProposalID: id, Choice: "approve"}).Error != "candidate_not_accepted" {
		t.Fatal("candidate skipped acceptance")
	}
	ok(t, call(s, "b", "accept_leadership", domain.Request{Key: "accept", ProposalID: id, Confirmed: true}))
	first := ok(t, call(s, "a", "vote_leadership", domain.Request{Key: "vote-a", ProposalID: id, Choice: "approve"}))
	if first.Leadership.Term != 1 || first.Leadership.Coordinator != "a" {
		t.Fatal("one vote created a majority", first.Leadership)
	}
	if call(s, "a", "vote_leadership", domain.Request{Key: "vote-again", ProposalID: id, Choice: "reject"}).Error != "already_voted" {
		t.Fatal("duplicate voter")
	}
	changed := ok(t, call(s, "b", "vote_leadership", domain.Request{Key: "vote-b", ProposalID: id, Choice: "approve"}))
	if changed.Leadership.Coordinator != "b" || changed.Leadership.Term != 2 {
		t.Fatal(changed.Leadership)
	}
	if call(s, "a", "create_task", domain.Request{Key: "stale-term", LeadershipTerm: 1, Title: "old lead", Acceptance: "do not use old authority"}).Error != "stale_term" {
		t.Fatal("old coordinator acted")
	}
	task := ok(t, call(s, "b", "create_task", domain.Request{Key: "new-term", LeadershipTerm: 2, Title: "new lead", Acceptance: "current term"}))
	if task.Task == nil {
		t.Fatal(task)
	}
}

func TestVoteRejectionFrozenAbsenceExpiryAndHumanOverride(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	// Only agents that called in the lease window join the fixed electorate.
	ok(t, call(s, "a", "get_session_state", domain.Request{}))
	ok(t, call(s, "b", "get_session_state", domain.Request{}))
	proposal := ok(t, call(s, "a", "propose_leadership", domain.Request{Key: "proposal", CandidateID: "b", Reason: "alternate approach"}))
	id := proposal.Leadership.ProposalID
	if proposal.Leadership.Required != 2 || len(proposal.Leadership.Electorate) != 2 {
		t.Fatal(proposal.Leadership)
	}
	ok(t, call(s, "b", "accept_leadership", domain.Request{Key: "accept", ProposalID: id, Confirmed: true}))
	ok(t, call(s, "a", "vote_leadership", domain.Request{Key: "no-a", ProposalID: id, Choice: "reject"}))
	rejected := ok(t, call(s, "b", "vote_leadership", domain.Request{Key: "no-b", ProposalID: id, Choice: "reject"}))
	if rejected.Leadership.Term != 1 || rejected.Leadership.Coordinator != "a" {
		t.Fatal(rejected.Leadership)
	}
	// Vote times out without reducing its frozen denominator.
	ok(t, call(s, "a", "get_session_state", domain.Request{}))
	ok(t, call(s, "b", "get_session_state", domain.Request{}))
	second := ok(t, call(s, "a", "propose_leadership", domain.Request{Key: "proposal-2", CandidateID: "b", Reason: "retry after rejection"}))
	if call(s, "a", "finalize_leadership", domain.Request{Key: "early", ProposalID: second.Leadership.ProposalID}).Error != "vote_still_open" {
		t.Fatal("proposal expired before deadline")
	}
	oldNow := s.now
	s.now = func() time.Time { return oldNow().Add(121 * time.Second) }
	expired := ok(t, call(s, "b", "finalize_leadership", domain.Request{Key: "expire", ProposalID: second.Leadership.ProposalID}))
	if expired.Leadership.ProposalID != "" || expired.Leadership.Term != 1 {
		t.Fatal(expired.Leadership)
	}
	// A human can impose a live candidate while recording a reason and term.
	forced := ok(t, call(s, "admin", "force_leadership", domain.Request{Key: "force", ExpectedVersion: 1, CandidateID: "c", Reason: "verified previous coordinator is unavailable"}))
	if forced.Leadership.Coordinator != "c" || forced.Leadership.Term != 2 {
		t.Fatal(forced.Leadership)
	}
	if call(s, "admin", "force_leadership", domain.Request{Key: "stale-force", ExpectedVersion: 1, CandidateID: "b", Reason: "stale"}).Error != "version_conflict" {
		t.Fatal("stale human override")
	}
}

func TestElectorateRejectsExpiredCandidateAndSingleVoter(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	ok(t, call(s, "a", "get_session_state", domain.Request{}))
	if call(s, "a", "propose_leadership", domain.Request{Key: "one", CandidateID: "a", Reason: "cannot renominate current"}).Error != "invalid_proposal" {
		t.Fatal("current coordinator nominated")
	}
	if call(s, "a", "propose_leadership", domain.Request{Key: "solo", CandidateID: "b", Reason: "not in electorate"}).Error != "candidate_unavailable" {
		t.Fatal("unseen candidate accepted")
	}
	s.now = func() time.Time { return time.Now().Add(100 * time.Second) }
	if len(ok(t, call(s, "a", "get_session_state", domain.Request{})).Participants) != 3 {
		t.Fatal("presence changed membership")
	}
}
