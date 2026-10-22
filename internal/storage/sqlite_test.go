package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func directory(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0700); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestPrivateFilesSymlinksAndSchema(t *testing.T) {
	dir := directory(t)
	path := filepath.Join(dir, "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var fk, version int
	var mode string
	if err = s.DB.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatal(fk, err)
	}
	if err = s.DB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatal(mode, err)
	}
	if err = s.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal(version, err)
	}
	for _, name := range []string{path, path + ".lock"} {
		info, e := os.Stat(name)
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatal("private file", info, e)
		}
	}
	if _, err = s.DB.Exec("INSERT INTO credentials(session_id,actor_id,token_hash) VALUES('missing','human',?)", string(make([]byte, 64))); err == nil {
		t.Fatal("foreign key disabled")
	}
	if _, err = s.DB.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if future, err := Open(path); err == nil {
		future.Close()
		t.Fatal("unknown future schema accepted")
	}
	linked := filepath.Join(dir, "linked.sqlite")
	if err = os.Symlink(path, linked); err != nil {
		t.Fatal(err)
	}
	if linkedDB, err := Open(linked); err == nil {
		linkedDB.Close()
		t.Fatal("database symlink followed")
	}
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if public, err := Open(filepath.Join(dir, "public.sqlite")); err == nil {
		public.Close()
		t.Fatal("public directory accepted")
	}
}
