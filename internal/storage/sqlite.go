// Package storage owns the SQLite schema and process-wide writer exclusion.
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB   *sql.DB
	lock *os.File
}

const schema = `
CREATE TABLE sessions (
 id TEXT PRIMARY KEY, objective TEXT NOT NULL, repo_path TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('prepared','active','closed')),
 version INTEGER NOT NULL CHECK(version>0), seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE agents (
 session_id TEXT NOT NULL REFERENCES sessions(id), id TEXT NOT NULL,
 name TEXT NOT NULL, metadata_json TEXT NOT NULL,
 activity TEXT NOT NULL DEFAULT 'available', last_agent_seen TEXT NOT NULL DEFAULT '',
 lease_until TEXT NOT NULL DEFAULT '', health TEXT NOT NULL DEFAULT 'unreachable',
 PRIMARY KEY(session_id,id), UNIQUE(session_id,name)
);
CREATE TABLE credentials (
 session_id TEXT NOT NULL REFERENCES sessions(id), actor_id TEXT NOT NULL,
 token_hash TEXT NOT NULL CHECK(length(token_hash)=64), revoked INTEGER NOT NULL DEFAULT 0 CHECK(revoked IN (0,1)),
 PRIMARY KEY(session_id,actor_id), UNIQUE(session_id,token_hash)
);
CREATE TABLE events (
 session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL,
 event_id TEXT NOT NULL UNIQUE, type TEXT NOT NULL, actor_id TEXT NOT NULL,
 schema_version INTEGER NOT NULL DEFAULT 1, request_id TEXT NOT NULL,
 payload_json TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(session_id,seq), FOREIGN KEY(session_id,actor_id) REFERENCES credentials(session_id,actor_id)
);
CREATE TRIGGER events_no_update BEFORE UPDATE ON events BEGIN SELECT RAISE(ABORT,'events are append-only'); END;
CREATE TRIGGER events_no_delete BEFORE DELETE ON events BEGIN SELECT RAISE(ABORT,'events are append-only'); END;
CREATE TABLE messages (
 session_id TEXT NOT NULL, id TEXT NOT NULL, sender_id TEXT NOT NULL,
 body TEXT NOT NULL, seq INTEGER NOT NULL,
 PRIMARY KEY(session_id,id), FOREIGN KEY(session_id,sender_id) REFERENCES credentials(session_id,actor_id),
 FOREIGN KEY(session_id,seq) REFERENCES events(session_id,seq)
);
CREATE TABLE message_receipts (
 session_id TEXT NOT NULL, message_id TEXT NOT NULL, recipient_id TEXT NOT NULL,
 first_delivered_at TEXT, acked_at TEXT,
 PRIMARY KEY(session_id,message_id,recipient_id),
 FOREIGN KEY(session_id,message_id) REFERENCES messages(session_id,id),
 FOREIGN KEY(session_id,recipient_id) REFERENCES credentials(session_id,actor_id)
);
CREATE INDEX inbox_pending ON message_receipts(session_id,recipient_id,acked_at);
CREATE TABLE idempotency (
 session_id TEXT NOT NULL, actor_id TEXT NOT NULL, key TEXT NOT NULL,
 request_hash TEXT NOT NULL, response_json TEXT NOT NULL,
 PRIMARY KEY(session_id,actor_id,key),
 FOREIGN KEY(session_id,actor_id) REFERENCES credentials(session_id,actor_id)
);
PRAGMA user_version=1;
`

const schema2 = `
ALTER TABLE sessions ADD COLUMN coordinator_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN leadership_term INTEGER NOT NULL DEFAULT 1;
UPDATE sessions SET coordinator_id=COALESCE((SELECT id FROM agents WHERE agents.session_id=sessions.id ORDER BY name LIMIT 1),'');
CREATE TABLE tasks (
 session_id TEXT NOT NULL REFERENCES sessions(id), id TEXT NOT NULL,
 parent_id TEXT, title TEXT NOT NULL, acceptance TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('proposed','assigned','accepted','working','blocked','review_requested','completed','failed','cancelled')),
 version INTEGER NOT NULL DEFAULT 1, blocked_from TEXT NOT NULL DEFAULT '',
 created_by TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(session_id,id),
 FOREIGN KEY(session_id,parent_id) REFERENCES tasks(session_id,id),
 FOREIGN KEY(session_id,created_by) REFERENCES credentials(session_id,actor_id)
);
CREATE TABLE assignments (
 session_id TEXT NOT NULL, id TEXT NOT NULL, task_id TEXT NOT NULL,
 agent_id TEXT NOT NULL, generation INTEGER NOT NULL, state TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
 PRIMARY KEY(session_id,id), UNIQUE(session_id,task_id,generation),
 FOREIGN KEY(session_id,task_id) REFERENCES tasks(session_id,id),
 FOREIGN KEY(session_id,agent_id) REFERENCES credentials(session_id,actor_id)
);
CREATE UNIQUE INDEX assignment_one_active ON assignments(session_id,task_id) WHERE state='active';
CREATE TABLE artifacts (
 session_id TEXT NOT NULL, id TEXT NOT NULL, task_id TEXT NOT NULL,
 author_id TEXT NOT NULL, kind TEXT NOT NULL, reference TEXT NOT NULL,
 digest TEXT NOT NULL, commit_sha TEXT NOT NULL DEFAULT '', task_version INTEGER NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(session_id,id),
 FOREIGN KEY(session_id,task_id) REFERENCES tasks(session_id,id),
 FOREIGN KEY(session_id,author_id) REFERENCES credentials(session_id,actor_id)
);
CREATE TABLE reviews (
 session_id TEXT NOT NULL, id TEXT NOT NULL, artifact_id TEXT NOT NULL,
 reviewer_id TEXT NOT NULL, verdict TEXT NOT NULL CHECK(verdict IN ('approved','changes_requested')),
 reason TEXT NOT NULL, artifact_digest TEXT NOT NULL, task_version INTEGER NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(session_id,id),
 FOREIGN KEY(session_id,artifact_id) REFERENCES artifacts(session_id,id),
 FOREIGN KEY(session_id,reviewer_id) REFERENCES credentials(session_id,actor_id)
);
CREATE TABLE leadership_proposals (
 session_id TEXT NOT NULL REFERENCES sessions(id), id TEXT NOT NULL,
 proposer_id TEXT NOT NULL, candidate_id TEXT NOT NULL, electorate_json TEXT NOT NULL,
 reason TEXT NOT NULL, opened_at TEXT NOT NULL, deadline TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','approved','rejected','expired')),
 candidate_accepted INTEGER NOT NULL DEFAULT 0 CHECK(candidate_accepted IN (0,1)),
 PRIMARY KEY(session_id,id),
 FOREIGN KEY(session_id,proposer_id) REFERENCES credentials(session_id,actor_id),
 FOREIGN KEY(session_id,candidate_id) REFERENCES credentials(session_id,actor_id)
);
CREATE UNIQUE INDEX leadership_one_pending ON leadership_proposals(session_id) WHERE status='pending';
CREATE TABLE votes (
 session_id TEXT NOT NULL, proposal_id TEXT NOT NULL, agent_id TEXT NOT NULL,
 choice TEXT NOT NULL CHECK(choice IN ('approve','reject')), seq INTEGER NOT NULL,
 PRIMARY KEY(session_id,proposal_id,agent_id),
 FOREIGN KEY(session_id,proposal_id) REFERENCES leadership_proposals(session_id,id),
 FOREIGN KEY(session_id,agent_id) REFERENCES credentials(session_id,actor_id),
 FOREIGN KEY(session_id,seq) REFERENCES events(session_id,seq)
);
CREATE TABLE workspaces (
 session_id TEXT NOT NULL REFERENCES sessions(id), id TEXT NOT NULL,
 owner_id TEXT NOT NULL, path TEXT NOT NULL, branch TEXT NOT NULL,
 base_sha TEXT NOT NULL, status TEXT NOT NULL,
 PRIMARY KEY(session_id,id), UNIQUE(session_id,path), UNIQUE(session_id,branch),
 FOREIGN KEY(session_id,owner_id) REFERENCES credentials(session_id,actor_id)
);
CREATE TABLE integration_locks (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id), owner_id TEXT NOT NULL,
 term INTEGER NOT NULL, workspace_id TEXT NOT NULL, status TEXT NOT NULL,
 started_at TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
 FOREIGN KEY(session_id,owner_id) REFERENCES credentials(session_id,actor_id),
 FOREIGN KEY(session_id,workspace_id) REFERENCES workspaces(session_id,id)
);
CREATE TABLE integrations (
 session_id TEXT NOT NULL, id TEXT NOT NULL, artifact_id TEXT NOT NULL,
 workspace_id TEXT NOT NULL, original_sha TEXT NOT NULL,
 integrated_sha TEXT NOT NULL, coordinator_id TEXT NOT NULL,
 term INTEGER NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(session_id,id),
 FOREIGN KEY(session_id,workspace_id) REFERENCES workspaces(session_id,id),
 FOREIGN KEY(session_id,coordinator_id) REFERENCES credentials(session_id,actor_id),
 FOREIGN KEY(session_id,artifact_id) REFERENCES artifacts(session_id,id)
);
PRAGMA user_version=2;
`

// Open keeps an advisory lock for the lifetime of the writer. The lock file is
// never unlinked, so a crashed daemon cannot leave a second lock inode behind.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("data directory must be a private real directory (0700)")
	}
	lock, err := privateFile(path + ".lock")
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another daemon owns this database")
	}
	s := &Store{lock: lock}
	file, err := privateFile(path)
	if err != nil {
		s.Close()
		return nil, err
	}
	file.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.DB = db
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA busy_timeout=5000"} {
		if _, err = db.Exec(q); err != nil {
			s.Close()
			return nil, err
		}
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		s.Close()
		return nil, err
	}
	if version > 2 {
		s.Close()
		return nil, fmt.Errorf("unsupported database schema %d", version)
	}
	if version == 0 {
		tx, err := db.Begin()
		if err != nil {
			s.Close()
			return nil, err
		}
		if _, err = tx.Exec(schema); err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			s.Close()
			return nil, err
		}
		version = 1
	}
	if version == 1 {
		tx, err := db.Begin()
		if err != nil {
			s.Close()
			return nil, err
		}
		if _, err = tx.Exec(schema2); err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}
func privateFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("database and lock must be regular private files (0600)")
	}
	return f, nil
}
func (s *Store) Close() error {
	var err error
	if s.DB != nil {
		err = s.DB.Close()
	}
	if s.lock != nil {
		if e := s.lock.Close(); err == nil {
			err = e
		}
	}
	return err
}
