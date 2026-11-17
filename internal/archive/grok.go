package archive

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func grokSession(ctx context.Context, h Host, root, session string) (Manifest, error) {
	m := Manifest{Version: 1, User: h.User, Host: h.Host, Harness: "grok", Root: root, NativeID: filepath.Base(session)}
	m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, m.NativeID)
	cwd, remote := "", ""
	summary := filepath.Join(session, "summary.json")
	if _, err := os.Stat(summary); err == nil {
		if err = safeSource(root, summary); err != nil {
			return m, err
		}
		f, err := os.Open(summary)
		if err != nil {
			return m, err
		}
		var v struct {
			Info struct {
				CWD string `json:"cwd"`
			} `json:"info"`
			Remotes []string `json:"git_remotes"`
		}
		err = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&v)
		f.Close()
		if err == nil {
			cwd = v.Info.CWD
			// Multiple unnamed remotes do not identify which repository is the origin.
			if len(v.Remotes) == 1 {
				remote = v.Remotes[0]
			}
		}
	}
	if cwd == "" {
		if decoded, err := url.PathUnescape(filepath.Base(filepath.Dir(session))); err == nil && filepath.IsAbs(decoded) {
			cwd = decoded
		}
	}
	m.CWD = cwd
	m.Project = Project(cwd, remote)
	err := filepath.WalkDir(session, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Lock files coordinate the live harness; they are not retained session data.
		if strings.HasSuffix(d.Name(), ".lock") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		return addMember(&m, p, rel, !strings.HasSuffix(p, ".jsonl"))
	})
	return m, err
}
