package archive

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sql(t *testing.T, db, query string) string {
	t.Helper()
	b, e := exec.Command("sqlite3", "-batch", db, query).CombinedOutput()
	if e != nil {
		t.Fatalf("sqlite: %v %s", e, b)
	}
	return strings.TrimSpace(string(b))
}
func TestOpenCodeSnapshots(t *testing.T) {
	if _, e := exec.LookPath("sqlite3"); e != nil {
		t.Fatal("sqlite3 required for database tests")
	}
	d := t.TempDir()
	db := filepath.Join(d, "opencode.db")
	cmd := exec.Command("sqlite3", "-batch", db)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	fmt.Fprintln(in, "PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE session(id TEXT PRIMARY KEY, directory TEXT); CREATE TABLE message(session_id TEXT); BEGIN; INSERT INTO session VALUES('s','/project'); INSERT INTO message VALUES('s'); COMMIT;\n.print READY")
	scan := bufio.NewScanner(out)
	ready := false
	for scan.Scan() {
		if scan.Text() == "READY" {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("writer not ready")
	}
	if _, e := os.Stat(db + "-wal"); e != nil {
		t.Fatal("WAL fixture missing", e)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			fmt.Fprintf(in, "BEGIN; INSERT INTO session VALUES('s%d','/project'); INSERT INTO message VALUES('s%d'); COMMIT;\n", i, i)
			time.Sleep(time.Millisecond)
		}
	}()
	h := Host{User: "u", Host: "h", Harnesses: map[string]Harness{"opencode": {Directories: []string{d}}}}
	inv := Discover(context.Background(), h, d)
	<-done
	in.Close()
	cmd.Wait()
	if len(inv.Errors) != 0 {
		t.Fatal(inv.Errors)
	}
	if len(inv.Traces) < 2 {
		t.Fatal("snapshot and sessions missing")
	}
	archive := filepath.Join(t.TempDir(), "archive")
	batch, e := NewIncoming(archive)
	if e != nil {
		t.Fatal(e)
	}
	if e = Prepare(context.Background(), batch, inv.Traces); e != nil {
		t.Fatal(e)
	}
	if e = Incorporate(archive, batch); e != nil {
		t.Fatal(e)
	}
	var capture Manifest
	for _, m := range inv.Traces {
		if m.SourceCapture {
			capture = m
		}
	}
	snap := filepath.Join(archive, capture.Dir(), "raw/opencode.db")
	if got := sql(t, snap, "PRAGMA integrity_check;"); got != "ok" {
		t.Fatal(got)
	}
	if got := sql(t, snap, "SELECT (SELECT count(*) FROM session) = (SELECT count(*) FROM message);"); got != "1" {
		t.Fatal("inconsistent snapshot", got)
	}
	if got := sql(t, snap, "SELECT count(*) FROM session WHERE id='s';"); got != "1" {
		t.Fatal("committed WAL row missing")
	}
	// A reset changes the database but cannot remove the retained original snapshot.
	sql(t, db, "DELETE FROM session; DELETE FROM message; INSERT INTO session VALUES('new','/project');")
	reset := Discover(context.Background(), h, d)
	if len(reset.Errors) != 0 {
		t.Fatal(reset.Errors)
	}
	old, e := LoadManifests(archive)
	if e != nil {
		t.Fatal(e)
	}
	selected := Select(reset.Traces, old, false)
	batch, _ = NewIncoming(archive)
	if e = Prepare(context.Background(), batch, selected); e != nil {
		t.Fatal(e)
	}
	if e = Incorporate(archive, batch); e != nil {
		t.Fatal(e)
	}
	if e = Verify(archive); e != nil {
		t.Fatal(e)
	}
	if got := sql(t, snap, "SELECT count(*) FROM session WHERE id='s';"); got != "1" {
		t.Fatal("old data lost")
	}
	again := Discover(context.Background(), h, d)
	old, _ = LoadManifests(archive)
	if s := Select(again.Traces, old, false); len(s) != 0 {
		t.Fatal("identical snapshot selected again")
	}
	for _, v := range []Inventory{inv, reset, again} {
		for _, p := range v.Scratch {
			os.RemoveAll(p)
		}
	}
}
func TestSQLiteOnlyRequiredForDatabaseHarness(t *testing.T) {
	d := t.TempDir()
	put(t, filepath.Join(d, "projects/p/s.jsonl"), "{}\n")
	t.Setenv("PATH", t.TempDir())
	h := Host{User: "u", Host: "h", Harnesses: map[string]Harness{"claude": {Directories: []string{d}}}}
	if inv := Discover(context.Background(), h, d); len(inv.Errors) != 0 || len(inv.Traces) != 1 {
		t.Fatal(inv)
	}
	put(t, filepath.Join(d, "opencode.db"), "")
	h.Harnesses = map[string]Harness{"opencode": {Directories: []string{d}}}
	if inv := Discover(context.Background(), h, d); len(inv.Errors) == 0 {
		t.Fatal("missing sqlite3 not reported")
	}
}
