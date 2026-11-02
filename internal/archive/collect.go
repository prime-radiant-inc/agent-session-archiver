package archive

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type Inventory struct {
	Traces []Manifest `json:"traces"`
	Errors []string   `json:"errors,omitempty"`
}

func Discover(ctx context.Context, h Host, home string) Inventory {
	var inv Inventory
	roots, err := h.Roots(home)
	if err != nil {
		inv.Errors = append(inv.Errors, err.Error())
		return inv
	}
	projects := map[string]string{}
	for name, dirs := range roots {
		for _, root := range dirs {
			if err := ctx.Err(); err != nil {
				inv.Errors = append(inv.Errors, err.Error())
				return inv
			}
			if _, err := os.Stat(root); err != nil {
				inv.Errors = append(inv.Errors, err.Error())
				continue
			}
			var walks []string
			switch name {
			case "claude":
				walks = []string{filepath.Join(root, "projects")}
			case "codex":
				index := filepath.Join(root, "session_index.jsonl")
				if _, e := os.Stat(index); e == nil {
					m := Manifest{Version: 1, User: h.User, Host: h.Host, Harness: name, Root: root, NativeID: "@session_index", Project: Component("shared codex metadata")}
					m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, m.NativeID)
					if e = addMember(&m, index, "session_index.jsonl", true); e != nil {
						inv.Errors = append(inv.Errors, e.Error())
					} else {
						inv.Traces = append(inv.Traces, m)
					}
				}
				for _, d := range []string{"sessions", "archived_sessions"} {
					p := filepath.Join(root, d)
					if _, e := os.Stat(p); e == nil {
						walks = append(walks, p)
					}
				}
			case "pi":
				walks = []string{filepath.Join(root, "sessions")}
			case "evener":
				entries, e := os.ReadDir(filepath.Join(root, "projects"))
				if e != nil {
					inv.Errors = append(inv.Errors, e.Error())
					continue
				}
				for _, v := range entries {
					if v.IsDir() {
						p := filepath.Join(root, "projects", v.Name(), "sessions")
						if _, e := os.Stat(p); e == nil {
							walks = append(walks, p)
						}
					}
				}
			case "opencode":
				inv.Errors = append(inv.Errors, "opencode capture not implemented")
				continue
			}
			for _, walk := range walks {
				err := filepath.WalkDir(walk, func(p string, d fs.DirEntry, e error) error {
					if e != nil {
						return e
					}
					if err := ctx.Err(); err != nil {
						return err
					}
					if d.Type()&os.ModeSymlink != 0 {
						return fmt.Errorf("symlink source refused: %s", p)
					}
					if d.IsDir() {
						return nil
					}
					if !strings.HasSuffix(p, ".jsonl") && !strings.HasSuffix(p, ".jsonl.zst") {
						return nil
					}
					if name == "evener" && !strings.HasSuffix(p, ".transcript.jsonl") {
						return nil
					}
					rel, e := filepath.Rel(root, p)
					if e != nil {
						return e
					}
					id, cwd := metadata(p, name)
					if name == "claude" {
						id = strings.TrimSuffix(filepath.Base(p), ".jsonl")
						if strings.Contains(rel, "/subagents/") {
							id = filepath.ToSlash(filepath.Dir(rel)) + "/" + id
						}
					}
					if id == "" {
						id = filepath.ToSlash(rel)
					}
					project, ok := projects[cwd]
					if !ok {
						project = Project(cwd)
						projects[cwd] = project
					}
					m := Manifest{Version: 1, User: h.User, Host: h.Host, Harness: name, Root: root, NativeID: id, CWD: cwd, Project: project}
					m.ID = TraceID(m.User, m.Host, m.Harness, m.Root, m.NativeID)
					if e = addMember(&m, p, rel, false); e != nil {
						return e
					}
					if name == "evener" {
						meta := strings.TrimSuffix(p, ".transcript.jsonl") + ".meta.json"
						if _, e := os.Stat(meta); e == nil {
							r, _ := filepath.Rel(root, meta)
							if e = addMember(&m, meta, r, true); e != nil {
								return e
							}
						}
					}
					if name == "claude" {
						side := strings.TrimSuffix(p, ".jsonl") + "/tool-results"
						if _, e := os.Stat(side); e == nil {
							if e = filepath.WalkDir(side, func(q string, v fs.DirEntry, e error) error {
								if e != nil {
									return e
								}
								if v.IsDir() {
									return nil
								}
								r, _ := filepath.Rel(root, q)
								return addMember(&m, q, r, true)
							}); e != nil {
								return e
							}
						}
					}
					inv.Traces = append(inv.Traces, m)
					return nil
				})
				if err != nil {
					inv.Errors = append(inv.Errors, fmt.Sprintf("%s: %v", walk, err))
				}
			}
		}
	}
	sort.Slice(inv.Traces, func(i, j int) bool { return inv.Traces[i].ID < inv.Traces[j].ID })
	return inv
}
func addMember(m *Manifest, p, rel string, mutable bool) error {
	if e := safeSource(m.Root, p); e != nil {
		return e
	}
	s, e := os.Stat(p)
	if e != nil {
		return e
	}
	if !s.Mode().IsRegular() {
		return fmt.Errorf("not regular: %s", p)
	}
	m.Members = append(m.Members, Member{Path: filepath.ToSlash(rel), Source: p, Size: s.Size(), Mtime: s.ModTime().UnixNano(), Mutable: mutable})
	return nil
}
func safeSource(root, p string) error {
	r, e := filepath.Rel(root, p)
	if e != nil || r == ".." || strings.HasPrefix(r, "../") {
		return fmt.Errorf("source escapes root: %s", p)
	}
	// A configured root may itself be a symlink; all descendants must be real files.
	at := root
	for _, part := range strings.Split(r, string(filepath.Separator)) {
		at = filepath.Join(at, part)
		s, e := os.Lstat(at)
		if e != nil {
			return e
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink source refused: %s", at)
		}
	}
	return nil
}
func metadata(p, name string) (string, string) {
	f, e := os.Open(p)
	if e != nil {
		return "", ""
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(p, ".zst") {
		z, e := zstd.NewReader(f, zstd.WithDecoderConcurrency(1))
		if e != nil {
			return "", ""
		}
		defer z.Close()
		r = z
	}
	scanner := bufio.NewScanner(io.LimitReader(r, 1024*1024))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	id, cwd := "", ""
	for n := 0; n < 32 && scanner.Scan(); n++ {
		var v map[string]json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &v) != nil {
			continue
		}
		if name == "codex" {
			var nested map[string]json.RawMessage
			if json.Unmarshal(v["payload"], &nested) == nil {
				v = nested
			}
		}
		for _, key := range []string{"session_id", "sessionId", "id"} {
			var s string
			if json.Unmarshal(v[key], &s) == nil && s != "" && id == "" {
				id = s
			}
		}
		for _, key := range []string{"cwd", "working_directory", "working_dir"} {
			var s string
			if json.Unmarshal(v[key], &s) == nil && s != "" {
				cwd = s
			}
		}
		if id != "" && cwd != "" {
			break
		}
	}
	return id, cwd
}
