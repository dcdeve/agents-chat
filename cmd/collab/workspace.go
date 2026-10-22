package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agents-chat/internal/domain"
	"agents-chat/internal/transport"
)

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(b)), err)
	}
	return strings.TrimSpace(string(b)), nil
}

func runWorkspace(ctx context.Context, action string, client *transport.Client, r domain.Request, owner string, integration bool, base string) error {
	switch action {
	case "list":
		r.Op = "list_workspaces"
	case "prepare":
		return prepareWorkspace(ctx, client, r, owner, integration, base)
	case "integrate":
		return integrateWorkspace(ctx, client, r)
	default:
		return errors.New("workspace subcommand must be prepare, list, or integrate")
	}
	out, err := client.Call(ctx, r)
	if err != nil {
		return err
	}
	return printResponse(out)
}

func printResponse(out domain.Response) error {
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return err
	}
	if out.Error != "" {
		return errors.New(out.Error)
	}
	return nil
}

func prepareWorkspace(ctx context.Context, client *transport.Client, r domain.Request, owner string, integration bool, base string) error {
	if client.Config.AgentID != "human" {
		return errors.New("workspace prepare requires the human admin credential")
	}
	if r.Key == "" {
		return errors.New("workspace prepare requires idempotency_key in --json")
	}
	state, err := client.Call(ctx, domain.Request{Op: "get_session_state"})
	if err != nil {
		return err
	}
	if state.Error != "" {
		return errors.New(state.Error)
	}
	if state.RepoPath == "" {
		return errors.New("session has no repository path")
	}
	repo, err := filepath.EvalSymlinks(state.RepoPath)
	if err != nil {
		return err
	}
	repo, err = filepath.Abs(repo)
	if err != nil {
		return err
	}
	root, err := git(ctx, repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if root != repo {
		return errors.New("configured repository path must be its Git top-level directory")
	}
	if owner == "" {
		if integration {
			if state.Leadership == nil {
				return errors.New("session has no current coordinator")
			}
			owner = state.Leadership.Coordinator
		} else {
			return errors.New("workspace prepare requires --owner participant UUID")
		}
	}
	if integration && (state.Leadership == nil || owner != state.Leadership.Coordinator) {
		return errors.New("integration worktree owner must be the current coordinator")
	}
	if !integration {
		known := false
		for _, p := range state.Participants {
			known = known || p.ID == owner
		}
		if !known {
			return errors.New("workspace owner is not a session participant")
		}
	}
	mode := owner
	if integration {
		mode = "integration-" + owner
	}
	id := domain.Hash(client.Config.Session + "/workspace/" + mode)[:32]
	branch := "collab/" + client.Config.Session + "/" + mode
	if base == "" {
		base, err = git(ctx, repo, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
	} else {
		base, err = git(ctx, repo, "rev-parse", "--verify", base+"^{commit}")
		if err != nil {
			return err
		}
	}
	workRoot := filepath.Join(filepath.Dir(client.Config.Socket), "worktrees")
	if err = os.MkdirAll(workRoot, 0700); err != nil {
		return err
	}
	if err = os.Chmod(workRoot, 0700); err != nil {
		return err
	}
	workRoot, err = filepath.Abs(workRoot)
	if err != nil {
		return err
	}
	if rel, e := filepath.Rel(repo, workRoot); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("session worktree directory must be outside the source repository")
	}
	path := filepath.Join(workRoot, id)
	if info, e := os.Lstat(path); e == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("existing worktree path is not a real directory")
		}
		got, e := git(ctx, path, "rev-parse", "--show-toplevel")
		if e != nil {
			return e
		}
		got, e = filepath.EvalSymlinks(got)
		if e != nil {
			return e
		}
		canonicalPath, e := filepath.EvalSymlinks(path)
		if e != nil {
			return e
		}
		if filepath.Clean(got) != filepath.Clean(canonicalPath) {
			return errors.New("existing worktree resolves to a different path")
		}
		got, e = git(ctx, path, "branch", "--show-current")
		if e != nil || got != branch {
			return errors.New("existing worktree has a different branch")
		}
	} else if !os.IsNotExist(e) {
		return e
	} else if _, e = git(ctx, repo, "worktree", "add", "-b", branch, path, base); e != nil {
		return e
	}
	w := domain.Workspace{ID: id, Owner: owner, Path: path, Branch: branch, BaseSHA: base, Status: "active"}
	state, err = client.Call(ctx, domain.Request{Op: "get_session_state"})
	if err != nil {
		return err
	}
	if state.Error != "" {
		return errors.New(state.Error)
	}
	r.Op = "register_workspace"
	r.ExpectedVersion = state.Version
	r.Workspace = &w
	out, err := client.Call(ctx, r)
	if err != nil {
		return err
	}
	return printResponse(out)
}

func integrateWorkspace(ctx context.Context, client *transport.Client, r domain.Request) error {
	if r.Key == "" || r.WorkspaceID == "" || r.TaskID == "" || r.ArtifactID == "" || r.LeadershipTerm < 1 {
		return errors.New("workspace integrate requires idempotency_key, leadership_term, workspace_id, task_id, and artifact_id")
	}
	r.Op = "begin_integration"
	started, err := client.Call(ctx, r)
	if err != nil {
		return err
	}
	if started.Error != "" {
		return errors.New(started.Error)
	}
	if len(started.Workspaces) != 1 || started.Artifact == nil {
		return errors.New("daemon returned incomplete integration data")
	}
	w := started.Workspaces[0]
	fail := func(cause error) error {
		_, finishErr := client.Call(ctx, domain.Request{Op: "finish_integration", Key: r.Key + ":failed", LeadershipTerm: r.LeadershipTerm, State: "failed", Reason: cause.Error()})
		if finishErr != nil {
			return fmt.Errorf("%v; could not record integration failure: %w", cause, finishErr)
		}
		return cause
	}
	status, err := git(ctx, w.Path, "status", "--porcelain")
	if err != nil {
		return fail(err)
	}
	if status != "" {
		return fail(errors.New("integration worktree is dirty; inspect it and use workspace recover after preserving any work"))
	}
	root, err := git(ctx, w.Path, "rev-parse", "--show-toplevel")
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	registeredPath, pathErr := filepath.EvalSymlinks(w.Path)
	if err != nil || pathErr != nil || filepath.Clean(root) != filepath.Clean(registeredPath) {
		return fail(errors.New("integration worktree path does not match its registration"))
	}
	if _, err = git(ctx, w.Path, "cherry-pick", started.Artifact.CommitSHA); err != nil {
		return fail(err)
	}
	sha, err := git(ctx, w.Path, "rev-parse", "HEAD")
	if err != nil {
		return fail(err)
	}
	finished, err := client.Call(ctx, domain.Request{Op: "finish_integration", Key: r.Key + ":complete", LeadershipTerm: r.LeadershipTerm, State: "completed", ResultSHA: sha})
	if err != nil {
		return err
	}
	return printResponse(finished)
}
