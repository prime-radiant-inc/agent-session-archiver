package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

type Member struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	Size    int64  `json:"size"`
	Mtime   int64  `json:"mtime"`
	SHA256  string `json:"sha256,omitempty"`
	Mutable bool   `json:"mutable,omitempty"`
}
type Manifest struct {
	Version  int      `json:"version"`
	ID       string   `json:"id"`
	User     string   `json:"user"`
	Host     string   `json:"host"`
	Harness  string   `json:"harness"`
	Root     string   `json:"root"`
	NativeID string   `json:"native_id"`
	CWD      string   `json:"cwd,omitempty"`
	Project  string   `json:"project"`
	Members  []Member `json:"members"`
	Snapshot string   `json:"snapshot,omitempty"`
}

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func TraceID(user, host, harness, root, id string) string {
	return digest(strings.Join([]string{user, host, harness, root, id}, "\x00"))
}
func Component(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 60 {
			break
		}
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-_")
	if slug == "" {
		slug = "unknown"
	}
	return slug + "--" + digest(s)[:16]
}
func NormalizeRemote(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		if i := strings.Index(s, ":"); i >= 0 {
			host := s[:i]
			if at := strings.LastIndex(host, "@"); at >= 0 {
				host = host[at+1:]
			}
			return strings.TrimSuffix(strings.ToLower(host)+"/"+strings.Trim(s[i+1:], "/"), ".git")
		}
		return s
	}
	u, e := url.Parse(s)
	if e != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname())+"/"+strings.Trim(u.Path, "/"), ".git")
}
func Project(cwd string) string {
	if cwd == "" {
		return Component("unknown")
	}
	run := func(args ...string) string {
		b, e := exec.Command("git", append([]string{"-C", cwd}, args...)...).Output()
		if e != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if remote := run("remote", "get-url", "origin"); remote != "" {
		return Component(NormalizeRemote(remote))
	}
	if common := run("rev-parse", "--path-format=absolute", "--git-common-dir"); common != "" {
		return Component(common)
	}
	return Component(cwd)
}
func (m Manifest) Dir() string { return filepath.Join(m.User, m.Host, m.Project, m.Harness, m.ID) }
func Select(in []Manifest, old map[string]Manifest, full bool) []Manifest {
	var selected []Manifest
	for _, m := range in {
		prev, exists := old[m.ID]
		if exists {
			m.Project = prev.Project
		}
		changed := full || !exists || m.Snapshot != ""
		members := map[string]Member{}
		for _, v := range prev.Members {
			members[v.Path] = v
		}
		m.Members = append([]Member(nil), m.Members...)
		for i := range m.Members {
			v := &m.Members[i]
			p, ok := members[v.Path]
			v.SHA256 = ""
			if !ok || p.Size != v.Size || p.Mtime != v.Mtime {
				changed = true
			} else if !full {
				v.SHA256 = p.SHA256
			}
		}
		if changed {
			selected = append(selected, m)
		}
	}
	return selected
}
