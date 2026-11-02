package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func discoverOpenCode(ctx context.Context, h Host, root string) ([]Manifest, string, error) {
	db := filepath.Join(root, "opencode.db")
	if e := safeSource(root, db); e != nil {
		return nil, "", e
	}
	scratch, e := os.MkdirTemp("", "agent-session-archiver-")
	if e != nil {
		return nil, "", e
	}
	uri := (&url.URL{Scheme: "file", Path: db}).String() + "?mode=ro"
	cmd := exec.CommandContext(ctx, "sqlite3", "-batch", "-readonly", "-init", "/dev/null", uri)
	cmd.Dir = scratch
	cmd.Stdin = strings.NewReader(".timeout 5000\n.backup snapshot.db\n")
	if b, e := cmd.CombinedOutput(); e != nil {
		os.RemoveAll(scratch)
		return nil, "", fmt.Errorf("SQLite backup: %w: %s", e, b)
	}
	snapshot := filepath.Join(scratch, "snapshot.db")
	sum, size, e := hashFile(snapshot)
	if e != nil {
		return nil, scratch, e
	}
	capture := Manifest{Version: 1, User: h.User, Host: h.Host, Harness: "opencode", Root: root, NativeID: "@snapshot:" + sum, Project: "source", Snapshot: sum, SourceCapture: true, CaptureRoot: scratch}
	capture.ID = TraceID(h.User, h.Host, "opencode", root, capture.NativeID)
	capture.Members = []Member{{Path: "opencode.db", Source: db, Size: size}}
	traces := []Manifest{capture}
	// The completed backup is self-contained and never changes during discovery.
	// Immutable mode avoids WAL/SHM sidecars, which Apple SQLite cannot create read-only.
	snapshotURI := (&url.URL{Scheme: "file", Path: snapshot}).String() + "?mode=ro&immutable=1"
	cmd = exec.CommandContext(ctx, "sqlite3", "-batch", "-readonly", "-init", "/dev/null", "-json", snapshotURI, "SELECT id, directory FROM session ORDER BY id;")
	b, e := cmd.CombinedOutput()
	if e != nil {
		return traces, scratch, fmt.Errorf("snapshot preserved but OpenCode session query failed: %w: %s", e, b)
	}
	var sessions []struct {
		ID        string `json:"id"`
		Directory string `json:"directory"`
	}
	if len(b) > 0 {
		if e = json.Unmarshal(b, &sessions); e != nil {
			return traces, scratch, e
		}
	}
	projects := map[string]string{}
	for _, s := range sessions {
		project, ok := projects[s.Directory]
		if !ok {
			project = Project(s.Directory, "")
			projects[s.Directory] = project
		}
		m := Manifest{Version: 1, User: h.User, Host: h.Host, Harness: "opencode", Root: root, NativeID: s.ID, CWD: s.Directory, Project: project, Snapshot: sum}
		m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, m.NativeID)
		traces = append(traces, m)
	}
	return traces, scratch, nil
}
func (m Manifest) sourceDir() string {
	return filepath.Join(m.User, m.Host, "_sources", "opencode", TraceID(m.User, m.Host, "opencode", m.Root, "@database"), m.Snapshot)
}
func (m Manifest) metadataName() string {
	if m.SourceCapture {
		return "source.json"
	}
	return "trace.json"
}
