package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// checkedPath rejects traversal and existing symlinks before filesystem operations.
// The configured root and its ancestors are trusted; archive descendants are not.
func checkedPath(root, rel string) (string, error) {
	if !filepath.IsLocal(rel) || filepath.Clean(rel) != rel {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	p := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		p = filepath.Join(p, part)
		s, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink refused: %s", p)
		}
	}
	return p, nil
}
func validateManifest(m Manifest) error {
	if m.Version != 1 || !validIdentity(m.User) || !validIdentity(m.Host) || !validIdentity(m.Project) || !validIdentity(m.ID) {
		return fmt.Errorf("invalid manifest identity/version")
	}
	if _, ok := defaults[m.Harness]; !ok {
		return fmt.Errorf("invalid harness %s", m.Harness)
	}
	if m.Snapshot != "" {
		if len(m.Snapshot) != 64 {
			return fmt.Errorf("invalid snapshot")
		}
		if _, e := hex.DecodeString(m.Snapshot); e != nil {
			return e
		}
	}
	if m.SourceCapture && (m.Harness != "opencode" || m.Snapshot == "") {
		return fmt.Errorf("invalid source capture")
	}
	seen := map[string]bool{}
	for _, v := range m.Members {
		if !filepath.IsLocal(v.Path) || filepath.Clean(v.Path) != v.Path || seen[v.Path] || v.Size < 0 || len(v.SHA256) != 64 {
			return fmt.Errorf("invalid member %q", v.Path)
		}
		if _, e := hex.DecodeString(v.SHA256); e != nil {
			return e
		}
		seen[v.Path] = true
	}
	return nil
}
func NewIncoming(root string) (string, error) {
	p, e := checkedPath(root, ".incoming")
	if e != nil {
		return "", e
	}
	if e = os.MkdirAll(p, 0700); e != nil {
		return "", e
	}
	return os.MkdirTemp(p, "batch-")
}
func validateBatchPath(root, batch string) error {
	abs, e := filepath.Abs(batch)
	if e != nil {
		return e
	}
	parent, e := filepath.Abs(filepath.Join(root, ".incoming"))
	if e != nil {
		return e
	}
	if filepath.Dir(abs) != parent || !strings.HasPrefix(filepath.Base(abs), "batch-") {
		return fmt.Errorf("batch outside incoming directory")
	}
	_, e = checkedPath(root, filepath.Join(".incoming", filepath.Base(abs)))
	return e
}
func lockHost(root, user, host string) (func(), error) {
	if !validIdentity(user) || !validIdentity(host) {
		return nil, fmt.Errorf("invalid namespace")
	}
	p, e := checkedPath(root, filepath.Join(".locks", user+"--"+host))
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("host archive busy: %w", e)
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
func hashFile(p string) (string, int64, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() {
		return "", 0, fmt.Errorf("not regular: %s", p)
	}
	h := sha256.New()
	n, e := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, e
}
func checkMember(p string, v Member) error {
	sum, n, e := hashFile(p)
	if e != nil {
		return e
	}
	if sum != v.SHA256 || n != v.Size {
		return fmt.Errorf("checksum/size mismatch: %s", p)
	}
	return nil
}
func batchMembers(root, batch string, traces []Manifest) error {
	seen := map[string]bool{}
	for _, m := range traces {
		if e := validateManifest(m); e != nil {
			return e
		}
		if seen[m.ID] {
			return fmt.Errorf("duplicate trace %s", m.ID)
		}
		seen[m.ID] = true
		for _, v := range m.Members {
			rel := filepath.Join(m.Dir(), "raw", v.Path)
			p, e := checkedPath(batch, rel)
			if e != nil {
				return e
			}
			if _, e = os.Stat(p); os.IsNotExist(e) {
				p, e = checkedPath(root, rel)
				if e != nil {
					return e
				}
			} else if e != nil {
				return e
			}
			if e = checkMember(p, v); e != nil {
				return e
			}
		}
	}
	return nil
}
func MarkReady(root, batch string) error {
	if e := validateBatchPath(root, batch); e != nil {
		return e
	}
	m, e := ReadBatch(batch)
	if e != nil {
		return e
	}
	if e = batchMembers(root, batch, m); e != nil {
		return e
	}
	// Rewriting the intended manifests durably is the recovery evidence.
	return writeJSON(filepath.Join(batch, "ready.json"), m)
}
func Incorporate(root, batch string) error { return incorporate(root, batch, installFile) }
func incorporate(root, batch string, install func(string, string) error) error {
	if e := validateBatchPath(root, batch); e != nil {
		return e
	}
	var traces []Manifest
	b, e := os.ReadFile(filepath.Join(batch, "ready.json"))
	if os.IsNotExist(e) {
		if e = MarkReady(root, batch); e != nil {
			return e
		}
		b, e = os.ReadFile(filepath.Join(batch, "ready.json"))
	}
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &traces); e != nil {
		return e
	}
	if len(traces) == 0 {
		return os.RemoveAll(batch)
	}
	user, host := traces[0].User, traces[0].Host
	for _, m := range traces {
		if m.User != user || m.Host != host {
			return fmt.Errorf("mixed host batch")
		}
	}
	release, e := lockHost(root, user, host)
	if e != nil {
		return e
	}
	defer release()
	if e = batchMembers(root, batch, traces); e != nil {
		return e
	}
	// Preflight every update before publishing any file from the batch.
	for _, m := range traces {
		for _, v := range m.Members {
			rel := filepath.Join(m.Dir(), "raw", v.Path)
			dest, e := checkedPath(root, rel)
			if e != nil {
				return e
			}
			src, e := checkedPath(batch, rel)
			if e != nil {
				return e
			}
			if _, e = os.Stat(dest); e == nil && !v.Mutable {
				if _, e = os.Stat(src); e == nil {
					if e = checkPrefix(dest, src); e != nil {
						return e
					}
				}
			} else if e != nil && !os.IsNotExist(e) {
				return e
			}
		}
	}
	for _, m := range traces {
		for _, v := range m.Members {
			rel := filepath.Join(m.Dir(), "raw", v.Path)
			dest, e := checkedPath(root, rel)
			if e != nil {
				return e
			}
			src, e := checkedPath(batch, rel)
			if e != nil {
				return e
			}
			if _, e = os.Stat(src); os.IsNotExist(e) {
				continue
			} else if e != nil {
				return e
			}
			if e = checkMember(dest, v); e == nil {
				continue
			}
			if e = install(src, dest); e != nil {
				return e
			}
		}
		path, e := checkedPath(root, filepath.Join(m.Dir(), m.metadataName()))
		if e != nil {
			return e
		}
		if old, e := readManifest(path); e == nil {
			have := map[string]bool{}
			for _, v := range m.Members {
				have[v.Path] = true
			}
			for _, v := range old.Members {
				if !have[v.Path] {
					m.Members = append(m.Members, v)
				}
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		m.CaptureRoot = ""
		if e = writeJSON(path, m); e != nil {
			return e
		}
	}
	return os.RemoveAll(batch)
}
func installFile(src, dest string) error {
	if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return e
	}
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.CreateTemp(filepath.Dir(dest), ".capture-")
	if e != nil {
		return e
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if _, e = io.Copy(out, in); e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(tmp, dest); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(dest))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func checkPrefix(old, new string) error {
	a, e := os.Open(old)
	if e != nil {
		return e
	}
	defer a.Close()
	b, e := os.Open(new)
	if e != nil {
		return e
	}
	defer b.Close()
	x := make([]byte, 65536)
	y := make([]byte, len(x))
	for {
		n, e := a.Read(x)
		if n > 0 {
			if _, err := io.ReadFull(b, y[:n]); err != nil || !bytes.Equal(x[:n], y[:n]) {
				return fmt.Errorf("transcript conflict; retaining %s", old)
			}
		}
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}
func Recover(root string) error {
	p, e := checkedPath(root, ".incoming")
	if e != nil {
		return e
	}
	entries, e := os.ReadDir(p)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var errs []error
	for _, v := range entries {
		if !v.IsDir() {
			continue
		}
		batch := filepath.Join(p, v.Name())
		if _, e = os.Stat(filepath.Join(batch, "ready.json")); e == nil {
			if e = Incorporate(root, batch); e != nil {
				errs = append(errs, fmt.Errorf("recover %s: %w", batch, e))
			}
		}
	}
	return errors.Join(errs...)
}
func readManifest(p string) (Manifest, error) {
	var m Manifest
	b, e := os.ReadFile(p)
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	if e == nil {
		e = validateManifest(m)
	}
	return m, e
}
func LoadManifests(root string) (map[string]Manifest, error) {
	out := map[string]Manifest{}
	e := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive symlink refused: %s", p)
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "raw" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "trace.json" && d.Name() != "source.json" {
			return nil
		}
		m, e := readManifest(p)
		if e != nil {
			return e
		}
		if filepath.Clean(p) != filepath.Join(root, m.Dir(), m.metadataName()) {
			return fmt.Errorf("manifest location mismatch: %s", p)
		}
		if _, ok := out[m.ID]; ok {
			return fmt.Errorf("duplicate archived identity %s", m.ID)
		}
		out[m.ID] = m
		return nil
	})
	if os.IsNotExist(e) {
		return out, nil
	}
	return out, e
}
func Verify(root string) error {
	all, e := LoadManifests(root)
	if e != nil {
		return e
	}
	var errs []error
	for _, m := range all {
		if m.Snapshot != "" && !m.SourceCapture {
			captureID := TraceID(m.User, m.Host, "opencode", m.Root, "@snapshot:"+m.Snapshot)
			if capture, ok := all[captureID]; !ok || !capture.SourceCapture {
				errs = append(errs, fmt.Errorf("missing snapshot for %s", m.ID))
			}
		}
		for _, v := range m.Members {
			p, e := checkedPath(root, filepath.Join(m.Dir(), "raw", v.Path))
			if e == nil {
				e = checkMember(p, v)
			}
			if e != nil {
				errs = append(errs, e)
			}
		}
	}
	return errors.Join(errs...)
}
