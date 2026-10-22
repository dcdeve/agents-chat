package application

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"agents-chat/internal/domain"
)

var revisionPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}([a-fA-F0-9]{24})?$`)

func (s *Service) leadership(tx *sql.Tx) (domain.Leadership, error) {
	var l domain.Leadership
	var proposal, candidate, elec, deadline string
	err := tx.QueryRow("SELECT coordinator_id,leadership_term FROM sessions WHERE id=?", s.session).Scan(&l.Coordinator, &l.Term)
	if err != nil {
		return l, err
	}
	err = tx.QueryRow("SELECT id,candidate_id,electorate_json,deadline FROM leadership_proposals WHERE session_id=? AND status='pending'", s.session).Scan(&proposal, &candidate, &elec, &deadline)
	if err == nil {
		l.ProposalID = proposal
		l.Candidate = candidate
		l.Deadline = instant(deadline)
		if err = jsonUnmarshal([]byte(elec), &l.Electorate); err != nil {
			return l, err
		}
		l.Required = len(l.Electorate)/2 + 1
		err = tx.QueryRow("SELECT count(*) FILTER(WHERE choice='approve'),count(*) FILTER(WHERE choice='reject') FROM votes WHERE session_id=? AND proposal_id=?", s.session, proposal).Scan(&l.VotesFor, &l.VotesAgainst)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return l, err
	}
	err = tx.QueryRow("SELECT owner_id,term,status,note FROM integration_locks WHERE session_id=?", s.session).Scan(&l.IntegrationOwner, &l.IntegrationTerm, &l.IntegrationStatus, &l.IntegrationNote)
	if errors.Is(err, sql.ErrNoRows) {
		return l, nil
	}
	return l, err
}

// jsonUnmarshal is a small indirection so the application remains the only
// package interpreting serialized proposal snapshots.
var jsonUnmarshal = func(data []byte, v any) error { return json.Unmarshal(data, v) }

func (s *Service) taskOperation(tx *sql.Tx, actor string, r domain.Request, out domain.Response) (domain.Response, error) {
	fail := func(code string) (domain.Response, error) { return domain.Response{}, fault(code) }
	if out.Status != "active" && r.Op != "list_tasks" && r.Op != "get_task" {
		return fail("session_not_active")
	}
	var currentCoordinator string
	var term int
	if err := tx.QueryRow("SELECT coordinator_id,leadership_term FROM sessions WHERE id=?", s.session).Scan(&currentCoordinator, &term); err != nil {
		return out, err
	}
	requireLeader := func() (domain.Response, error) {
		if (actor != currentCoordinator && actor != "human") || r.LeadershipTerm != term {
			return fail("stale_term")
		}
		return out, nil
	}
	if r.Op == "list_tasks" || r.Op == "get_task" {
		query := "SELECT id,COALESCE(parent_id,''),title,acceptance,state,version,blocked_from FROM tasks WHERE session_id=?"
		args := []any{s.session}
		if r.Op == "get_task" {
			query += " AND id=?"
			args = append(args, r.TaskID)
		}
		query += " ORDER BY created_at,id"
		rows, err := tx.Query(query, args...)
		if err != nil {
			return out, err
		}
		defer rows.Close()
		for rows.Next() {
			var t domain.Task
			if err = rows.Scan(&t.ID, &t.ParentID, &t.Title, &t.Acceptance, &t.State, &t.Version, &t.BlockedFrom); err != nil {
				return out, err
			}
			err = tx.QueryRow("SELECT id,agent_id,generation FROM assignments WHERE session_id=? AND task_id=? AND state='active'", s.session, t.ID).Scan(&t.AssignmentID, &t.Assignee, &t.Generation)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return out, err
			}
			out.Tasks = append(out.Tasks, t)
		}
		if err = rows.Err(); err != nil {
			return out, err
		}
		if r.Op == "get_task" && len(out.Tasks) == 0 {
			return fail("not_found")
		}
		return out, nil
	}
	switch r.Op {
	case "create_task":
		if _, e := requireLeader(); e != nil {
			return out, e
		}
		if r.Title == "" || len(r.Title) > 512 || r.Acceptance == "" || len(r.Acceptance) > 8192 {
			return fail("invalid_task")
		}
		id := domain.ID()
		if r.ParentID != "" {
			var found string
			if err := tx.QueryRow("SELECT id FROM tasks WHERE session_id=? AND id=?", s.session, r.ParentID).Scan(&found); errors.Is(err, sql.ErrNoRows) {
				return fail("not_found")
			} else if err != nil {
				return out, err
			}
		}
		_, err := tx.Exec("INSERT INTO tasks(session_id,id,parent_id,title,acceptance,state,version,created_by,created_at) VALUES(?,?,?,?,?,'proposed',1,?,?)", s.session, id, nullString(r.ParentID), r.Title, r.Acceptance, actor, stamp(s.now()))
		if err != nil {
			return out, err
		}
		task := domain.Task{ID: id, ParentID: r.ParentID, Title: r.Title, Acceptance: r.Acceptance, State: "proposed", Version: 1}
		if _, err = s.event(tx, actor, "task_created", r.RequestID, task); err != nil {
			return out, err
		}
		out.Task = &task
	case "assign_task":
		if _, e := requireLeader(); e != nil {
			return out, e
		}
		if r.AgentID == "" || r.AgentID == "human" || r.Generation < 0 {
			return fail("invalid_assignment")
		}
		task, err := readTask(tx, s.session, r.TaskID)
		if err != nil {
			return out, err
		}
		if task.Version != r.TaskVersion {
			return fail("version_conflict")
		}
		var status string
		if err = tx.QueryRow("SELECT actor_id FROM credentials WHERE session_id=? AND actor_id=? AND revoked=0", s.session, r.AgentID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		} else if err != nil {
			return out, err
		}
		if task.State != "proposed" && task.State != "assigned" {
			return fail("invalid_transition")
		}
		var generation int
		if err = tx.QueryRow("SELECT COALESCE(max(generation),0)+1 FROM assignments WHERE session_id=? AND task_id=?", s.session, task.ID).Scan(&generation); err != nil {
			return out, err
		}
		if _, err = tx.Exec("UPDATE assignments SET state='superseded',reason=? WHERE session_id=? AND task_id=? AND state='active'", r.Reason, s.session, task.ID); err != nil {
			return out, err
		}
		assignment := domain.ID()
		at := stamp(s.now())
		if _, err = tx.Exec("INSERT INTO assignments(session_id,id,task_id,agent_id,generation,state,created_at) VALUES(?,?,?,?,?,'active',?)", s.session, assignment, task.ID, r.AgentID, generation, at); err != nil {
			return out, err
		}
		task.State = "assigned"
		task.Version++
		task.Assignee = r.AgentID
		task.Generation = generation
		task.AssignmentID = assignment
		if _, err = tx.Exec("UPDATE tasks SET state='assigned',version=? WHERE session_id=? AND id=?", task.Version, s.session, task.ID); err != nil {
			return out, err
		}
		if _, err = s.event(tx, actor, "task_assigned", r.RequestID, task); err != nil {
			return out, err
		}
		text := fmt.Sprintf("Task %s assigned to you (assignment %d). Acceptance: %s", task.ID, generation, task.Acceptance)
		if _, err = s.send(tx, actor, r.AgentID, text, r.RequestID); err != nil {
			return out, err
		}
		out.Task = &task
	case "accept_task":
		task, assignment, err := assignedTask(tx, s.session, r.TaskID, actor, r.Generation)
		if err != nil {
			return out, err
		}
		if task.Version != r.TaskVersion {
			return fail("version_conflict")
		}
		if task.State != "assigned" {
			return fail("invalid_transition")
		}
		task.State = "accepted"
		task.Version++
		if _, err = tx.Exec("UPDATE tasks SET state='accepted',version=? WHERE session_id=? AND id=?", task.Version, s.session, task.ID); err != nil {
			return out, err
		}
		if _, err = s.event(tx, actor, "task_accepted", r.RequestID, map[string]any{"task": task, "assignment_id": assignment}); err != nil {
			return out, err
		}
		out.Task = &task
	case "update_task":
		if r.State == "completed" || r.State == "failed" || r.State == "cancelled" {
			if _, e := requireLeader(); e != nil {
				return out, e
			}
			task, err := readTask(tx, s.session, r.TaskID)
			if err != nil {
				return out, err
			}
			if task.Version != r.TaskVersion {
				return fail("version_conflict")
			}
			if task.State == "completed" || task.State == "failed" || task.State == "cancelled" {
				return fail("invalid_transition")
			}
			if (r.State == "failed" || r.State == "cancelled") && r.Reason == "" {
				return fail("reason_required")
			}
			if r.State == "completed" {
				if task.State != "review_requested" {
					return fail("invalid_transition")
				}
				var artifactID, author, digest, reviewDigest, verdict, reviewer string
				var artifactVersion, reviewVersion int
				e := tx.QueryRow("SELECT a.id,a.author_id,a.digest,a.task_version,r.artifact_digest,r.verdict,r.reviewer_id,r.task_version FROM artifacts a JOIN reviews r ON r.session_id=a.session_id AND r.artifact_id=a.id WHERE a.session_id=? AND a.task_id=? ORDER BY r.rowid DESC LIMIT 1", s.session, task.ID).Scan(&artifactID, &author, &digest, &artifactVersion, &reviewDigest, &verdict, &reviewer, &reviewVersion)
				if errors.Is(e, sql.ErrNoRows) {
					return fail("review_required")
				}
				if e != nil {
					return out, e
				}
				if verdict != "approved" || reviewer == author || digest != reviewDigest || reviewVersion != task.Version-1 || artifactVersion != reviewVersion-1 {
					return fail("review_stale")
				}
			}
			task.State = r.State
			task.Version++
			if _, err = tx.Exec("UPDATE tasks SET state=?,version=? WHERE session_id=? AND id=?", task.State, task.Version, s.session, task.ID); err != nil {
				return out, err
			}
			if _, err = tx.Exec("UPDATE assignments SET state='finished' WHERE session_id=? AND task_id=? AND state='active'", s.session, task.ID); err != nil {
				return out, err
			}
			if _, err = s.event(tx, actor, "task_"+task.State, r.RequestID, map[string]any{"task": task, "reason": r.Reason}); err != nil {
				return out, err
			}
			if err = tx.QueryRow("SELECT seq FROM sessions WHERE id=?", s.session).Scan(&out.Seq); err != nil {
				return out, err
			}
			out.Task = &task
			return out, nil
		}
		task, assignment, err := assignedTask(tx, s.session, r.TaskID, actor, r.Generation)
		if err != nil {
			return out, err
		}
		if task.Version != r.TaskVersion {
			return fail("version_conflict")
		}
		if !((task.State == "accepted" && r.State == "working") || (task.State == "working" && r.State == "blocked") || (task.State == "blocked" && (r.State == "working" || r.State == "accepted"))) {
			return fail("invalid_transition")
		}
		if r.State == "blocked" && r.Reason == "" {
			return fail("reason_required")
		}
		if r.State == "blocked" {
			task.BlockedFrom = task.State
		} else {
			task.BlockedFrom = ""
		}
		task.State = r.State
		task.Version++
		if _, err = tx.Exec("UPDATE tasks SET state=?,version=?,blocked_from=? WHERE session_id=? AND id=?", task.State, task.Version, task.BlockedFrom, s.session, task.ID); err != nil {
			return out, err
		}
		if _, err = s.event(tx, actor, "task_updated", r.RequestID, map[string]any{"task": task, "assignment_id": assignment, "reason": r.Reason}); err != nil {
			return out, err
		}
		out.Task = &task
	case "publish_artifact":
		task, assignment, err := assignedTask(tx, s.session, r.TaskID, actor, r.Generation)
		if err != nil {
			return out, err
		}
		if task.Version != r.TaskVersion {
			return fail("version_conflict")
		}
		if task.State != "working" {
			return fail("invalid_transition")
		}
		if r.Artifact == nil || r.Artifact.Kind == "" || r.Artifact.Reference == "" || !domain.ValidHash(r.Artifact.Digest) {
			return fail("invalid_artifact")
		}
		if r.Artifact.Kind != "document" && r.Artifact.Kind != "file" && r.Artifact.Kind != "diff" && r.Artifact.Kind != "commit" {
			return fail("invalid_artifact")
		}
		if r.Artifact.Kind == "commit" && !revisionPattern.MatchString(r.Artifact.CommitSHA) {
			return fail("invalid_artifact")
		}
		artifact := *r.Artifact
		artifact.ID = domain.ID()
		artifact.TaskID = task.ID
		artifact.Author = actor
		artifact.TaskVersion = task.Version + 1
		if _, err = tx.Exec("UPDATE tasks SET version=version+1 WHERE session_id=? AND id=?", s.session, task.ID); err != nil {
			return out, err
		}
		task.Version++
		if _, err = tx.Exec("INSERT INTO artifacts(session_id,id,task_id,author_id,kind,reference,digest,commit_sha,task_version,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", s.session, artifact.ID, task.ID, actor, artifact.Kind, artifact.Reference, artifact.Digest, artifact.CommitSHA, artifact.TaskVersion, stamp(s.now())); err != nil {
			return out, err
		}
		if _, err = s.event(tx, actor, "artifact_published", r.RequestID, artifact); err != nil {
			return out, err
		}
		out.Task = &task
		out.Artifact = &artifact
		_ = assignment
	case "request_review":
		task, assignment, err := assignedTask(tx, s.session, r.TaskID, actor, r.Generation)
		if err != nil {
			return out, err
		}
		if task.Version != r.TaskVersion {
			return fail("version_conflict")
		}
		if task.State != "working" {
			return fail("invalid_transition")
		}
		var artifact domain.Artifact
		err = tx.QueryRow("SELECT id,author_id,kind,reference,digest,commit_sha,task_version FROM artifacts WHERE session_id=? AND task_id=? ORDER BY rowid DESC LIMIT 1", s.session, task.ID).Scan(&artifact.ID, &artifact.Author, &artifact.Kind, &artifact.Reference, &artifact.Digest, &artifact.CommitSHA, &artifact.TaskVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("artifact_required")
		}
		if err != nil {
			return out, err
		}
		if artifact.TaskVersion != task.Version || artifact.Author != actor {
			return fail("artifact_stale")
		}
		task.State = "review_requested"
		task.Version++
		if _, err = tx.Exec("UPDATE tasks SET state=?,version=? WHERE session_id=? AND id=?", task.State, task.Version, s.session, task.ID); err != nil {
			return out, err
		}
		if _, err = s.event(tx, actor, "review_requested", r.RequestID, map[string]any{"task": task, "assignment_id": assignment, "artifact_id": artifact.ID}); err != nil {
			return out, err
		}
		var coord string
		_ = tx.QueryRow("SELECT coordinator_id FROM sessions WHERE id=?", s.session).Scan(&coord)
		if coord != "" && coord != actor {
			if _, err = s.send(tx, actor, coord, "Review requested for task "+task.ID, r.RequestID); err != nil {
				return out, err
			}
		}
		out.Task = &task
		out.Artifact = &artifact
	case "submit_review":
		if r.Verdict != "approved" && r.Verdict != "changes_requested" {
			return fail("invalid_verdict")
		}
		if r.Reason == "" {
			return fail("reason_required")
		}
		var task domain.Task
		var author, digest string
		var artifactVersion int
		err := tx.QueryRow("SELECT t.id,COALESCE(t.parent_id,''),t.title,t.acceptance,t.state,t.version,t.blocked_from,a.author_id,a.digest,a.task_version FROM tasks t JOIN artifacts a ON a.session_id=t.session_id AND a.task_id=t.id WHERE t.session_id=? AND t.id=? AND a.id=?", s.session, r.TaskID, r.ArtifactID).Scan(&task.ID, &task.ParentID, &task.Title, &task.Acceptance, &task.State, &task.Version, &task.BlockedFrom, &author, &digest, &artifactVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		if actor == author {
			return fail("independent_review_required")
		}
		if task.State != "review_requested" {
			return fail("invalid_transition")
		}
		if task.Version != r.TaskVersion {
			return fail("version_conflict")
		}
		var latestID string
		if err = tx.QueryRow("SELECT id FROM artifacts WHERE session_id=? AND task_id=? ORDER BY rowid DESC LIMIT 1", s.session, task.ID).Scan(&latestID); err != nil {
			return out, err
		}
		if latestID != r.ArtifactID || artifactVersion != task.Version-1 {
			return fail("artifact_stale")
		}
		review := domain.Review{ID: domain.ID(), ArtifactID: r.ArtifactID, Reviewer: actor, Verdict: r.Verdict, Reason: r.Reason}
		if _, err = tx.Exec("INSERT INTO reviews(session_id,id,artifact_id,reviewer_id,verdict,reason,artifact_digest,task_version,created_at) VALUES(?,?,?,?,?,?,?,?,?)", s.session, review.ID, review.ArtifactID, actor, review.Verdict, review.Reason, digest, task.Version, stamp(s.now())); err != nil {
			return out, err
		}
		if r.Verdict == "changes_requested" {
			task.State = "working"
		}
		task.Version++
		if _, err = tx.Exec("UPDATE tasks SET state=?,version=? WHERE session_id=? AND id=?", task.State, task.Version, s.session, task.ID); err != nil {
			return out, err
		}
		if _, err = s.event(tx, actor, "review_submitted", r.RequestID, review); err != nil {
			return out, err
		}
		out.Task = &task
		out.Review = &review
	default:
		return fail("not_found")
	}
	return out, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func readTask(tx *sql.Tx, session, id string) (domain.Task, error) {
	var t domain.Task
	err := tx.QueryRow("SELECT id,COALESCE(parent_id,''),title,acceptance,state,version,blocked_from FROM tasks WHERE session_id=? AND id=?", session, id).Scan(&t.ID, &t.ParentID, &t.Title, &t.Acceptance, &t.State, &t.Version, &t.BlockedFrom)
	if errors.Is(err, sql.ErrNoRows) {
		return t, fault("not_found")
	}
	return t, err
}
func assignedTask(tx *sql.Tx, session, id, actor string, generation int) (domain.Task, string, error) {
	t, err := readTask(tx, session, id)
	if err != nil {
		return t, "", err
	}
	var assignment, assignee string
	var current int
	err = tx.QueryRow("SELECT id,agent_id,generation FROM assignments WHERE session_id=? AND task_id=? AND state='active'", session, id).Scan(&assignment, &assignee, &current)
	if errors.Is(err, sql.ErrNoRows) || assignee != actor {
		return t, "", fault("stale_assignment")
	}
	if err != nil {
		return t, "", err
	}
	if generation != current {
		return t, "", fault("stale_assignment")
	}
	t.AssignmentID = assignment
	t.Assignee = assignee
	t.Generation = current
	return t, assignment, nil
}

func (s *Service) send(tx *sql.Tx, actor, to, body, request string) (domain.Message, error) {
	var ok int
	err := tx.QueryRow("SELECT count(*) FROM credentials WHERE session_id=? AND actor_id=? AND revoked=0", s.session, to).Scan(&ok)
	if err != nil {
		return domain.Message{}, err
	}
	if ok != 1 {
		return domain.Message{}, fault("not_found")
	}
	m := domain.Message{ID: domain.ID(), Sender: actor, Body: body, Recipients: []string{to}}
	if err = tx.QueryRow("SELECT seq+1 FROM sessions WHERE id=?", s.session).Scan(&m.Seq); err != nil {
		return m, err
	}
	if _, err = s.event(tx, actor, "message", request, m); err != nil {
		return m, err
	}
	if _, err = tx.Exec("INSERT INTO messages(session_id,id,sender_id,body,seq) VALUES(?,?,?,?,?)", s.session, m.ID, actor, m.Body, m.Seq); err != nil {
		return m, err
	}
	_, err = tx.Exec("INSERT INTO message_receipts(session_id,message_id,recipient_id) VALUES(?,?,?)", s.session, m.ID, to)
	return m, err
}
