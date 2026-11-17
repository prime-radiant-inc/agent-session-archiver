package archive

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// batchManifest is the stream's final entry. Its arrival is the only evidence
// that every member before it was captured and transferred completely.
const batchManifest = "batch.json"

// linkFile shares received members; tests replace it to exercise the copy fallback.
var linkFile = os.Link

// WriteBatch captures the selected members of traces as a tar stream. Each member
// is read through one descriptor, bounded to its size when opened, and hashed as
// it is written; the completed manifests follow as the last entry. A capture
// failure ends the stream without that entry, so receivers never publish it.
func WriteBatch(ctx context.Context, w io.Writer, traces []Manifest) error {
	type captured struct {
		name   string
		member Member
	}
	tw := tar.NewWriter(w)
	copies := map[string]captured{}
	for i := range traces {
		m := &traces[i]
		for j := range m.Members {
			if err := ctx.Err(); err != nil {
				return err
			}
			v := &m.Members[j]
			if v.SHA256 != "" {
				continue
			}
			sourceRoot, source := m.Root, v.Source
			if m.SourceCapture {
				sourceRoot = m.CaptureRoot
				source = filepath.Join(sourceRoot, "snapshot.db")
			}
			if err := safeSource(sourceRoot, source); err != nil {
				return err
			}
			key, e := filepath.EvalSymlinks(source)
			if e != nil {
				return e
			}
			name := path.Join(filepath.ToSlash(m.Dir()), "raw", filepath.ToSlash(v.Path))
			if prior, ok := copies[key]; ok && prior.member.Mutable == v.Mutable {
				if e = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: name, Linkname: prior.name, Mode: 0600}); e != nil {
					return e
				}
				v.Size = prior.member.Size
				v.Mtime = prior.member.Mtime
				v.SHA256 = prior.member.SHA256
				copies[key] = captured{name, *v}
				continue
			}
			f, e := os.Open(source)
			if e != nil {
				return e
			}
			info, e := f.Stat()
			if e == nil {
				e = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: info.Size(), Mode: 0600})
			}
			var sum string
			if e == nil {
				sum, e = captureOpened(f, info, tw, v.Mutable)
			}
			f.Close()
			if e != nil {
				return e
			}
			v.Size = info.Size()
			v.Mtime = info.ModTime().UnixNano()
			if m.SourceCapture {
				v.Mtime = 0
			}
			v.SHA256 = sum
			copies[key] = captured{name, *v}
		}
	}
	b, e := json.MarshalIndent(traces, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	if e = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: batchManifest, Size: int64(len(b)), Mode: 0600}); e != nil {
		return e
	}
	if _, e = tw.Write(b); e != nil {
		return e
	}
	return tw.Close()
}

// captureOpened copies exactly the size f had when info was taken. Append-only
// growth after that is picked up by a later capture; a mutable member must not
// change at all while it is read.
func captureOpened(f *os.File, info os.FileInfo, w io.Writer, mutable bool) (string, error) {
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular source")
	}
	h := sha256.New()
	if _, e := io.CopyN(io.MultiWriter(w, h), f, info.Size()); e != nil {
		return "", e
	}
	after, e := f.Stat()
	if e != nil {
		return "", e
	}
	if after.Size() < info.Size() || mutable && (after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime())) {
		return "", fmt.Errorf("source changed during capture: %s", f.Name())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ReceiveBatch unpacks a WriteBatch stream into an empty incoming batch
// directory. It accepts only batch.json and regular files or hard links to
// earlier entries under a trace's raw directory, so a source cannot place
// recovery markers such as ready.json. batch.json is published only once the
// stream has ended cleanly after it. Members are not flushed: incorporation
// verifies them and publishes durable copies.
func ReceiveBatch(r io.Reader, batch string) error {
	tr := tar.NewReader(r)
	received := map[string]string{}
	manifest := ""
	defer func() {
		if manifest != "" {
			os.Remove(manifest)
		}
	}()
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if manifest != "" {
			return fmt.Errorf("batch stream continues after %s", batchManifest)
		}
		if !filepath.IsLocal(h.Name) || path.Clean(h.Name) != h.Name || received[h.Name] != "" {
			return fmt.Errorf("invalid batch entry %q", h.Name)
		}
		if h.Name == batchManifest {
			if h.Typeflag != tar.TypeReg {
				return fmt.Errorf("invalid %s entry", batchManifest)
			}
			f, e := os.CreateTemp(batch, ".batch-")
			if e != nil {
				return e
			}
			manifest = f.Name()
			_, e = io.Copy(f, tr)
			if e == nil {
				e = f.Sync()
			}
			if e = errors.Join(e, f.Close()); e != nil {
				return e
			}
			continue
		}
		if i := strings.Index(h.Name, "/raw/"); i <= 0 || i+len("/raw/") == len(h.Name) {
			return fmt.Errorf("batch entry %q is not a trace member", h.Name)
		}
		dest, e := checkedPath(batch, filepath.FromSlash(h.Name))
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		switch h.Typeflag {
		case tar.TypeReg:
			e = receiveFile(tr, dest)
		case tar.TypeLink:
			prior := received[h.Linkname]
			if prior == "" {
				return fmt.Errorf("batch entry %q links to unknown %q", h.Name, h.Linkname)
			}
			e = receiveLink(prior, dest)
		default:
			return fmt.Errorf("unsupported batch entry %q", h.Name)
		}
		if e != nil {
			return e
		}
		received[h.Name] = dest
	}
	if manifest == "" {
		return fmt.Errorf("batch stream ended without %s", batchManifest)
	}
	if e := renameSynced(manifest, filepath.Join(batch, batchManifest)); e != nil {
		return e
	}
	manifest = ""
	return nil
}
func receiveFile(r io.Reader, dest string) error {
	out, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, r)
	return errors.Join(e, out.Close())
}

// receiveLink shares an earlier member, copying it where links are unavailable.
func receiveLink(prior, dest string) error {
	if linkFile(prior, dest) == nil {
		return nil
	}
	f, e := os.Open(prior)
	if e != nil {
		return e
	}
	defer f.Close()
	return receiveFile(f, dest)
}
func ReadBatch(dir string) ([]Manifest, error) {
	var m []Manifest
	b, e := os.ReadFile(filepath.Join(dir, "batch.json"))
	if e != nil {
		return nil, e
	}
	e = json.Unmarshal(b, &m)
	return m, e
}
func writeJSON(p string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return writeFile(p, append(b, '\n'))
}

// writeFile publishes b at p through a synced temporary file and rename.
func writeFile(p string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".metadata-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return renameSynced(tmp, p)
}

// renameSynced moves a synced file into place and flushes the directory entry.
func renameSynced(tmp, p string) error {
	if e := os.Rename(tmp, p); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
