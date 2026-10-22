package application

import (
	"strings"
	"testing"

	"agents-chat/internal/domain"
)

func TestTaskAssignmentReviewAndCompletion(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	created := ok(t, call(s, "a", "create_task", domain.Request{Key: "task", LeadershipTerm: 1, Title: "change behavior", Acceptance: "tests pass"}))
	task := *created.Task
	child := ok(t, call(s, "a", "create_task", domain.Request{Key: "child", LeadershipTerm: 1, Title: "investigate", Acceptance: "publish evidence", ParentID: task.ID}))
	if child.Task.ParentID != task.ID {
		t.Fatal(child)
	}
	assigned := ok(t, call(s, "a", "assign_task", domain.Request{Key: "assign-1", LeadershipTerm: 1, TaskID: task.ID, AgentID: "b", TaskVersion: task.Version}))
	task = *assigned.Task
	generation := task.Generation
	if len(ok(t, call(s, "b", "check_inbox", domain.Request{})).Messages) != 1 {
		t.Fatal("assignment did not notify")
	}
	ok(t, call(s, "b", "accept_task", domain.Request{Key: "accept", TaskID: task.ID, Generation: generation, TaskVersion: task.Version}))
	task = *ok(t, call(s, "b", "update_task", domain.Request{Key: "work", TaskID: task.ID, Generation: generation, TaskVersion: task.Version + 1, State: "working"})).Task
	digest := domain.Hash("review this exact artifact")
	artifact := ok(t, call(s, "b", "publish_artifact", domain.Request{Key: "artifact-1", TaskID: task.ID, Generation: generation, TaskVersion: task.Version, Artifact: &domain.Artifact{Kind: "diff", Reference: "git diff abc123", Digest: digest}})).Artifact
	task = *ok(t, call(s, "b", "request_review", domain.Request{Key: "request-review", TaskID: task.ID, Generation: generation, TaskVersion: task.Version + 1})).Task
	if call(s, "b", "submit_review", domain.Request{Key: "self-review", TaskID: task.ID, ArtifactID: artifact.ID, TaskVersion: task.Version, Verdict: "approved", Reason: "looks good"}).Error != "independent_review_required" {
		t.Fatal("author reviewed own artifact")
	}
	if call(s, "a", "update_task", domain.Request{Key: "premature", LeadershipTerm: 1, TaskID: task.ID, TaskVersion: task.Version, State: "completed"}).Error != "review_required" {
		t.Fatal("coordinator completed without review")
	}
	reviewed := ok(t, call(s, "c", "submit_review", domain.Request{Key: "review-1", TaskID: task.ID, ArtifactID: artifact.ID, TaskVersion: task.Version, Verdict: "changes_requested", Reason: "add regression coverage"}))
	task = *reviewed.Task
	if task.State != "working" {
		t.Fatal(task)
	}
	secondArtifact := ok(t, call(s, "b", "publish_artifact", domain.Request{Key: "artifact-2", TaskID: task.ID, Generation: generation, TaskVersion: task.Version, Artifact: &domain.Artifact{Kind: "commit", Reference: "f0f0", CommitSHA: strings.Repeat("a", 40), Digest: domain.Hash("corrected")}})).Artifact
	task = ok(t, call(s, "b", "get_task", domain.Request{TaskID: task.ID})).Tasks[0]
	task = *ok(t, call(s, "b", "request_review", domain.Request{Key: "request-review-2", TaskID: task.ID, Generation: generation, TaskVersion: task.Version})).Task
	task = *ok(t, call(s, "c", "submit_review", domain.Request{Key: "review-2", TaskID: task.ID, ArtifactID: secondArtifact.ID, TaskVersion: task.Version, Verdict: "approved", Reason: "correction resolves the issue"})).Task
	latestResponse := ok(t, call(s, "b", "get_task", domain.Request{TaskID: task.ID}))
	latest := latestResponse.Tasks[0]
	if latest.State != "review_requested" {
		t.Fatal(latest)
	}
	completion := ok(t, call(s, "a", "update_task", domain.Request{Key: "complete", LeadershipTerm: 1, TaskID: task.ID, TaskVersion: latest.Version, State: "completed"}))
	if completion.Task.State != "completed" {
		t.Fatal(completion)
	}
	if got := call(s, "b", "update_task", domain.Request{Key: "stale-worker", TaskID: task.ID, Generation: generation, TaskVersion: latest.Version, State: "working"}); got.Error != "stale_assignment" {
		t.Fatal(got)
	}
	listed := ok(t, call(s, "a", "list_tasks", domain.Request{}))
	if len(listed.Tasks) != 2 {
		t.Fatal(listed)
	}
}

func TestReassignmentFencesOldWorkerAndOptimisticVersions(t *testing.T) {
	s, _, _ := fixture(t)
	defer s.Close()
	task := *ok(t, call(s, "a", "create_task", domain.Request{Key: "t", LeadershipTerm: 1, Title: "write", Acceptance: "done"})).Task
	first := *ok(t, call(s, "a", "assign_task", domain.Request{Key: "a1", LeadershipTerm: 1, TaskID: task.ID, TaskVersion: task.Version, AgentID: "b"})).Task
	second := *ok(t, call(s, "a", "assign_task", domain.Request{Key: "a2", LeadershipTerm: 1, TaskID: task.ID, TaskVersion: first.Version, AgentID: "c", Reason: "capacity changed"})).Task
	if second.Generation != first.Generation+1 {
		t.Fatal(first, second)
	}
	if call(s, "b", "accept_task", domain.Request{Key: "old", TaskID: task.ID, Generation: first.Generation, TaskVersion: second.Version}).Error != "stale_assignment" {
		t.Fatal("old assignee kept authority")
	}
	if call(s, "c", "accept_task", domain.Request{Key: "conflict", TaskID: task.ID, Generation: second.Generation, TaskVersion: first.Version}).Error != "version_conflict" {
		t.Fatal("stale task version accepted")
	}
	accepted := ok(t, call(s, "c", "accept_task", domain.Request{Key: "new", TaskID: task.ID, Generation: second.Generation, TaskVersion: second.Version}))
	if accepted.Task.Assignee != "c" {
		t.Fatal(accepted)
	}
}
