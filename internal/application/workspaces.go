package application

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"

	"agents-chat/internal/domain"
)

var workspaceRevision = regexp.MustCompile(`^[a-fA-F0-9]{40}([a-fA-F0-9]{24})?$`)

func (s *Service) workspaceOperation(tx *sql.Tx, actor string, r domain.Request, out domain.Response) (domain.Response, error) {
	fail := func(code string) (domain.Response, error) { return domain.Response{}, fault(code) }
	var coordinator string
	var term int
	if err := tx.QueryRow("SELECT coordinator_id,leadership_term FROM sessions WHERE id=?", s.session).Scan(&coordinator, &term); err != nil {
		return out, err
	}
	switch r.Op {
	case "list_workspaces":
		rows, err := tx.Query("SELECT id,owner_id,path,branch,base_sha,status FROM workspaces WHERE session_id=? ORDER BY owner_id,id", s.session)
		if err != nil {
			return out, err
		}
		defer rows.Close()
		for rows.Next() {
			var w domain.Workspace
			if err = rows.Scan(&w.ID, &w.Owner, &w.Path, &w.Branch, &w.BaseSHA, &w.Status); err != nil {
				return out, err
			}
			out.Workspaces = append(out.Workspaces, w)
		}
		return out, rows.Err()
	case "register_workspace":
		if actor != "human" {
			return fail("unauthorized")
		}
		if r.Workspace == nil {
			return fail("invalid_workspace")
		}
		w := *r.Workspace
		if w.ID == "" || w.Owner == "" || w.Path == "" || w.Branch == "" || !workspaceRevision.MatchString(w.BaseSHA) || w.Status != "active" {
			return fail("invalid_workspace")
		}
		_, err := tx.Exec("INSERT INTO workspaces(session_id,id,owner_id,path,branch,base_sha,status) VALUES(?,?,?,?,?,?,?)", s.session, w.ID, w.Owner, w.Path, w.Branch, w.BaseSHA, w.Status)
		if err != nil {
			var existing int
			if e := tx.QueryRow("SELECT count(*) FROM workspaces WHERE session_id=? AND id=?", s.session, w.ID).Scan(&existing); e == nil && existing > 0 {
				return fail("already_exists")
			}
			return out, err
		}
		if _, err = s.event(tx, actor, "workspace_registered", r.RequestID, w); err != nil {
			return out, err
		}
		out.Workspaces = []domain.Workspace{w}
	case "begin_integration":
		if actor != coordinator || r.LeadershipTerm != term {
			return fail("stale_term")
		}
		var status string
		if err := tx.QueryRow("SELECT status FROM sessions WHERE id=?", s.session).Scan(&status); err != nil {
			return out, err
		}
		if status != "active" {
			return fail("session_not_active")
		}
		var w domain.Workspace
		err := tx.QueryRow("SELECT id,owner_id,path,branch,base_sha,status FROM workspaces WHERE session_id=? AND id=?", s.session, r.WorkspaceID).Scan(&w.ID, &w.Owner, &w.Path, &w.Branch, &w.BaseSHA, &w.Status)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		if w.Owner != actor || w.Status != "active" {
			return fail("invalid_workspace")
		}
		var state, author, kind, reference, digest, sha string
		var artifactVersion int
		err = tx.QueryRow("SELECT t.state,a.author_id,a.kind,a.reference,a.digest,a.commit_sha,a.task_version FROM tasks t JOIN artifacts a ON a.session_id=t.session_id AND a.task_id=t.id WHERE t.session_id=? AND t.id=? AND a.id=?", s.session, r.TaskID, r.ArtifactID).Scan(&state, &author, &kind, &reference, &digest, &sha, &artifactVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		if state != "completed" || kind != "commit" || !workspaceRevision.MatchString(sha) {
			return fail("artifact_not_integrable")
		}
		var latest string
		if err = tx.QueryRow("SELECT id FROM artifacts WHERE session_id=? AND task_id=? ORDER BY rowid DESC LIMIT 1", s.session, r.TaskID).Scan(&latest); err != nil {
			return out, err
		}
		if latest != r.ArtifactID {
			return fail("artifact_stale")
		}
		var verdict, reviewDigest, reviewer string
		var reviewVersion int
		err = tx.QueryRow("SELECT verdict,artifact_digest,reviewer_id,task_version FROM reviews WHERE session_id=? AND artifact_id=? ORDER BY rowid DESC LIMIT 1", s.session, r.ArtifactID).Scan(&verdict, &reviewDigest, &reviewer, &reviewVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("review_required")
		}
		if err != nil {
			return out, err
		}
		if verdict != "approved" || reviewDigest != digest || reviewer == author || reviewVersion != artifactVersion+1 {
			return fail("review_stale")
		}
		var open int
		if err = tx.QueryRow("SELECT count(*) FROM integration_locks WHERE session_id=?", s.session).Scan(&open); err != nil {
			return out, err
		}
		if open > 0 {
			return fail("integration_locked")
		}
		if _, err = tx.Exec("INSERT INTO integration_locks(session_id,owner_id,term,workspace_id,status,started_at) VALUES(?,?,?,?,'active',?)", s.session, actor, term, w.ID, stamp(s.now())); err != nil {
			return out, err
		}
		artifact := domain.Artifact{ID: r.ArtifactID, TaskID: r.TaskID, Author: author, Kind: kind, Reference: reference, Digest: digest, CommitSHA: sha, TaskVersion: artifactVersion}
		if _, err = s.event(tx, actor, "integration_started", r.RequestID, map[string]any{"artifact": artifact, "workspace": w, "term": term}); err != nil {
			return out, err
		}
		out.Workspaces = []domain.Workspace{w}
		out.Artifact = &artifact
	case "finish_integration":
		if actor != coordinator || r.LeadershipTerm != term {
			return fail("stale_term")
		}
		var owner, wid, state string
		var lockTerm int
		err := tx.QueryRow("SELECT owner_id,workspace_id,status,term FROM integration_locks WHERE session_id=?", s.session).Scan(&owner, &wid, &state, &lockTerm)
		if errors.Is(err, sql.ErrNoRows) {
			return fail("not_found")
		}
		if err != nil {
			return out, err
		}
		if owner != actor || lockTerm != term || state != "active" {
			return fail("stale_term")
		}
		if r.State == "failed" {
			if r.Reason == "" {
				return fail("reason_required")
			}
			if _, err = tx.Exec("UPDATE integration_locks SET status='recovery',note=? WHERE session_id=?", r.Reason, s.session); err != nil {
				return out, err
			}
			_, err = s.event(tx, actor, "integration_conflict", r.RequestID, map[string]string{"workspace_id": wid, "note": r.Reason})
		} else if r.State == "completed" {
			if !workspaceRevision.MatchString(r.ResultSHA) {
				return fail("invalid_revision")
			}
			var payload string
			if err = tx.QueryRow("SELECT payload_json FROM events WHERE session_id=? AND type='integration_started' ORDER BY seq DESC LIMIT 1", s.session).Scan(&payload); err != nil {
				return out, err
			}
			var detail struct {
				Artifact domain.Artifact `json:"artifact"`
			}
			if err = json.Unmarshal([]byte(payload), &detail); err != nil {
				return out, err
			}
			if _, err = tx.Exec("INSERT INTO integrations(session_id,id,artifact_id,workspace_id,original_sha,integrated_sha,coordinator_id,term,created_at) VALUES(?,?,?,?,?,?,?,?,?)", s.session, domain.ID(), detail.Artifact.ID, wid, detail.Artifact.CommitSHA, r.ResultSHA, actor, term, stamp(s.now())); err != nil {
				return out, err
			}
			if _, err = tx.Exec("UPDATE workspaces SET base_sha=? WHERE session_id=? AND id=?", r.ResultSHA, s.session, wid); err != nil {
				return out, err
			}
			if _, err = tx.Exec("DELETE FROM integration_locks WHERE session_id=?", s.session); err != nil {
				return out, err
			}
			_, err = s.event(tx, actor, "integration_completed", r.RequestID, map[string]string{"artifact_id": detail.Artifact.ID, "original_sha": detail.Artifact.CommitSHA, "integrated_sha": r.ResultSHA, "workspace_id": wid})
		} else {
			return fail("invalid_transition")
		}
	case "recover_integration":
		if actor != "human" || !r.Confirmed || r.Reason == "" {
			return fail("human_confirmation_required")
		}
		var exists int
		if err := tx.QueryRow("SELECT count(*) FROM integration_locks WHERE session_id=?", s.session).Scan(&exists); err != nil {
			return out, err
		}
		if exists == 0 {
			return fail("not_found")
		}
		if _, err := tx.Exec("DELETE FROM integration_locks WHERE session_id=?", s.session); err != nil {
			return out, err
		}
		if _, err := s.event(tx, actor, "integration_recovered", r.RequestID, map[string]string{"reason": r.Reason, "confirmation": "human verified prior Git process stopped and worktree inspected"}); err != nil {
			return out, err
		}
	}
	if err := tx.QueryRow("SELECT seq FROM sessions WHERE id=?", s.session).Scan(&out.Seq); err != nil {
		return out, err
	}
	if l, e := s.leadership(tx); e == nil {
		out.Leadership = &l
	} else {
		return out, e
	}
	return out, nil
}
