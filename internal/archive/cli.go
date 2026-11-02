package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Run(ctx context.Context, args []string, out, errout io.Writer) int {
	config := ""
	full, all := false, false
	var pos []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			fmt.Fprintln(out, "agent-session-archiver --config FILE pull HOST|--all\nagent-session-archiver --config FILE push HOST SSH-ALIAS:/archive\nagent-session-archiver --config FILE verify\nUse --full with push/pull to bypass metadata shortcuts.")
			return 0
		case "--config":
			i++
			if i == len(args) {
				fmt.Fprintln(errout, "--config requires a path")
				return 2
			}
			config = args[i]
		case "--full":
			full = true
		case "--all":
			all = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintln(errout, "unknown option:", args[i])
				return 2
			}
			pos = append(pos, args[i])
		}
	}
	if len(pos) == 0 || (pos[0] != "pull" && pos[0] != "push" && pos[0] != "verify") {
		fmt.Fprintln(errout, "expected push, pull or verify")
		return 2
	}
	if config == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			fmt.Fprintln(errout, e)
			return 2
		}
		config = filepath.Join(home, ".config", "agent-session-archiver", "config.toml")
	}
	b, e := os.ReadFile(config)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	c, e := ParseConfig(b)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	if pos[0] != "push" && !filepath.IsAbs(c.Archive) {
		fmt.Fprintln(errout, "archive must be an absolute local path")
		return 2
	}
	if pos[0] == "verify" {
		if len(pos) != 1 || all || full {
			return 2
		}
		if e = Verify(c.Archive); e != nil {
			fmt.Fprintln(errout, e)
			return 1
		}
		fmt.Fprintln(out, "archive verified")
		return 0
	}
	var hosts []Host
	if pos[0] == "pull" && all && len(pos) == 1 {
		hosts = c.Hosts
	} else {
		want := 2
		if pos[0] == "push" {
			want = 3
		}
		if len(pos) != want || all {
			fmt.Fprintln(errout, "invalid command arguments")
			return 2
		}
		for _, h := range c.Hosts {
			if h.Host == pos[1] {
				hosts = append(hosts, h)
			}
		}
	}
	if len(hosts) == 0 {
		fmt.Fprintln(errout, "no matching configured hosts")
		return 2
	}
	if pos[0] == "push" {
		if _, _, e = destination(pos[2]); e != nil {
			fmt.Fprintln(errout, e)
			return 2
		}
	}
	status := 0
	for _, h := range hosts {
		var r Result
		if pos[0] == "pull" {
			r, e = Pull(ctx, h, c.Archive, full)
		} else {
			r, e = Push(ctx, h, pos[2], full)
		}
		fmt.Fprintf(out, "%s: discovered=%d selected=%d\n", h.Host, r.Discovered, r.Selected)
		if e != nil {
			fmt.Fprintf(errout, "%s: %v\n", h.Host, e)
			status = 1
		}
		if ctx.Err() != nil {
			break
		}
	}
	return status
}
