package archive

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Archive string `toml:"archive"`
	Hosts   []Host `toml:"hosts"`
}
type Host struct {
	SSH       string             `toml:"ssh"`
	User      string             `toml:"user"`
	Host      string             `toml:"host"`
	Harnesses map[string]Harness `toml:"harnesses"`
}
type Harness struct {
	Directories []string `toml:"directories"`
	Archives    []string `toml:"archives"`
}

type SourceRoot struct {
	Path    string
	Archive bool
}

var defaults = map[string]string{"claude": ".claude", "codex": ".codex", "pi": ".pi/agent", "evener": ".local/state/evener", "opencode": ".local/share/opencode", "lace": ".lace", "grok": ".grok"}
var identityPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$`)

func validIdentity(s string) bool { return identityPattern.MatchString(s) }
func ParseConfig(b []byte) (Config, error) {
	var c Config
	if err := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(&c); err != nil {
		return c, err
	}
	seen := map[string]bool{}
	for _, h := range c.Hosts {
		if !validIdentity(h.User) || !validIdentity(h.Host) || seen[h.Host] {
			return c, fmt.Errorf("invalid or duplicate host identity %q", h.Host)
		}
		seen[h.Host] = true
		for name, v := range h.Harnesses {
			if _, ok := defaults[name]; !ok {
				return c, fmt.Errorf("unsupported harness %q", name)
			}
			if (v.Directories != nil && len(v.Directories) == 0) || (v.Archives != nil && len(v.Archives) == 0) {
				return c, fmt.Errorf("empty directories for %s", name)
			}
		}
	}
	return c, nil
}
func (h Host) Roots(home string) (map[string][]SourceRoot, error) {
	roots := map[string][]SourceRoot{}
	for name, v := range h.Harnesses {
		def, ok := defaults[name]
		if !ok {
			return nil, fmt.Errorf("unsupported harness %s", name)
		}
		var dirs []SourceRoot
		if name == "opencode" && v.Archives != nil {
			return nil, fmt.Errorf("opencode uses directories containing opencode.db")
		}
		for _, p := range v.Directories {
			dirs = append(dirs, SourceRoot{Path: p})
		}
		for _, p := range v.Archives {
			dirs = append(dirs, SourceRoot{Path: p, Archive: true})
		}
		if v.Directories == nil && v.Archives == nil {
			dirs = []SourceRoot{{Path: filepath.Join(home, def)}}
		}
		if len(dirs) == 0 {
			return nil, fmt.Errorf("empty roots")
		}
		seen := map[string]bool{}
		for _, source := range dirs {
			d := source.Path
			if d == "~" {
				d = home
			} else if strings.HasPrefix(d, "~/") {
				d = filepath.Join(home, d[2:])
			}
			if !filepath.IsAbs(d) {
				return nil, fmt.Errorf("root must be absolute: %q", d)
			}
			d = filepath.Clean(d)
			if seen[d] {
				return nil, fmt.Errorf("duplicate root %s", d)
			}
			seen[d] = true
			source.Path = d
			roots[name] = append(roots[name], source)
		}
	}
	return roots, nil
}
