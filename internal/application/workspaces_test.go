package application

import (
	"strings"
	"testing"

	"agents-chat/internal/domain"
)

func TestIntegrationRequiresIndependentApprovalAndHumanRecovery(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	task := *ok(t, call(s, "a", "create_task", domain.Request{Key: "t", LeadershipTerm: 1, Title: "fix", Acceptance: "regression test passes"})).Task
	task = *ok(t, call(s, "a", "assign_task", domain.Request{Key: "assign", LeadershipTerm: 1, TaskID: task.ID, AgentID: "b", TaskVersion: task.Version})).Task
	generation := task.Generation
	ok(t, call(s, "b", "accept_task", domain.Request{Key: "accept", TaskID: task.ID, Generation: generation, TaskVersion: task.Version}))
	task = *ok(t, call(s, "b", "update_task", domain.Request{Key: "working", TaskID: task.ID, Generation: generation, TaskVersion: task.Version + 1, State: "working"})).Task
	sha := strings.Repeat("a", 40)
	artifact := ok(t, call(s, "b", "publish_artifact", domain.Request{Key: "artifact", TaskID: task.ID, Generation: generation, TaskVersion: task.Version, Artifact: &domain.Artifact{Kind: "commit", Reference: sha, CommitSHA: sha, Digest: domain.Hash("diff")}})).Artifact
	task = *ok(t, call(s, "b", "request_review", domain.Request{Key: "review-request", TaskID: task.ID, Generation: generation, TaskVersion: task.Version + 1})).Task
	task = *ok(t, call(s, "c", "submit_review", domain.Request{Key: "review", TaskID: task.ID, ArtifactID: artifact.ID, TaskVersion: task.Version, Verdict: "approved", Reason: "verified regression test"})).Task
	task = *ok(t, call(s, "a", "update_task", domain.Request{Key: "done", LeadershipTerm: 1, TaskID: task.ID, TaskVersion: task.Version, State: "completed"})).Task
	w := domain.Workspace{ID: "integration", Owner: "a", Path: "/repo-integration", Branch: "collab/s/integration", BaseSHA: strings.Repeat("b", 40), Status: "active"}
	state := ok(t, call(s, "admin", "get_session_state", domain.Request{}))
	ok(t, call(s, "admin", "register_workspace", domain.Request{Key: "workspace", ExpectedVersion: state.Version, Workspace: &w}))
	in := domain.Request{Key: "integrate-1", LeadershipTerm: 1, WorkspaceID: w.ID, TaskID: task.ID, ArtifactID: artifact.ID}
	ok(t, call(s, "a", "begin_integration", in))
	if got := call(s, "a", "begin_integration", domain.Request{Key: "integrate-2", LeadershipTerm: 1, WorkspaceID: w.ID, TaskID: task.ID, ArtifactID: artifact.ID}); got.Error != "integration_locked" {
		t.Fatal("second integration entered", got)
	}
	ok(t, call(s, "a", "finish_integration", domain.Request{Key: "conflict", LeadershipTerm: 1, State: "failed", Reason: "cherry-pick conflict"}))
	if got := call(s, "a", "begin_integration", domain.Request{Key: "integrate-3", LeadershipTerm: 1, WorkspaceID: w.ID, TaskID: task.ID, ArtifactID: artifact.ID}); got.Error != "integration_locked" {
		t.Fatal("recovery lock was released", got)
	}
	if got := call(s, "a", "recover_integration", domain.Request{Key: "unsafe", Reason: "checked"}); got.Error != "unauthorized" {
		t.Fatal("coordinator recovered lock", got)
	}
	state = ok(t, call(s, "admin", "get_session_state", domain.Request{}))
	ok(t, call(s, "admin", "recover_integration", domain.Request{Key: "recover", ExpectedVersion: state.Version, Confirmed: true, Reason: "verified Git stopped and worktree inspected"}))
	ok(t, call(s, "a", "begin_integration", domain.Request{Key: "integrate-4", LeadershipTerm: 1, WorkspaceID: w.ID, TaskID: task.ID, ArtifactID: artifact.ID}))
	result := strings.Repeat("c", 40)
	ok(t, call(s, "a", "finish_integration", domain.Request{Key: "complete", LeadershipTerm: 1, State: "completed", ResultSHA: result}))
	var base string
	if err := s.store.DB.QueryRow("SELECT base_sha FROM workspaces WHERE session_id='s' AND id='integration'").Scan(&base); err != nil || base != result {
		t.Fatal("integrated base was not recorded", base, err)
	}
}
