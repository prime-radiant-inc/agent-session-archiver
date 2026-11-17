package archive

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSSHPushPull(t *testing.T) {
	if os.Getenv("ASA_SSH_TEST") != "1" {
		t.Skip("skipped: set ASA_SSH_TEST=1 for real SSH integration")
	}
	for _, tool := range []string{"ssh", "sshd", "ssh-keygen"} {
		if _, e := exec.LookPath(tool); e != nil {
			t.Fatal(e)
		}
	}
	d := t.TempDir()
	bin := filepath.Join(d, "bin")
	os.MkdirAll(bin, 0700)
	run := func(name string, args ...string) {
		t.Helper()
		b, e := exec.Command(name, args...).CombinedOutput()
		if e != nil {
			t.Fatalf("%s: %s %v", name, b, e)
		}
	}
	run("go", "build", "-o", filepath.Join(bin, "agent-session-archiver"), "../../cmd/agent-session-archiver")
	run("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(d, "hostkey"))
	run("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(d, "key"))
	pub, _ := os.ReadFile(filepath.Join(d, "key.pub"))
	put(t, filepath.Join(d, "authorized_keys"), string(pub))
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	u, e := user.Current()
	if e != nil {
		t.Fatal(e)
	}
	cfg := fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nAllowUsers %s\nSetEnv PATH=%s:/usr/bin:/bin\n", port, filepath.Join(d, "hostkey"), filepath.Join(d, "pid"), filepath.Join(d, "authorized_keys"), u.Username, bin)
	put(t, filepath.Join(d, "sshd_config"), cfg)
	sshd, _ := exec.LookPath("sshd")
	cmd := exec.Command(sshd, "-D", "-e", "-f", filepath.Join(d, "sshd_config"))
	log, e := os.Create(filepath.Join(d, "sshd.log"))
	if e != nil {
		t.Fatal(e)
	}
	defer log.Close()
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			b, _ := os.ReadFile(log.Name())
			t.Log(string(b))
		}
	})
	ready := false
	for i := 0; i < 100; i++ {
		c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if e == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("sshd not ready")
	}
	hostPub, _ := os.ReadFile(filepath.Join(d, "hostkey.pub"))
	fields := strings.Fields(string(hostPub))
	put(t, filepath.Join(d, "known_hosts"), fmt.Sprintf("[127.0.0.1]:%d %s %s\n", port, fields[0], fields[1]))
	client := fmt.Sprintf("Host fixture\n HostName 127.0.0.1\n Port %d\n User %s\n IdentityFile %s\n IdentitiesOnly yes\n UserKnownHostsFile %s\n StrictHostKeyChecking yes\nHost offline\n HostName 127.0.0.1\n Port 1\n", port, u.Username, filepath.Join(d, "key"), filepath.Join(d, "known_hosts"))
	put(t, filepath.Join(d, "ssh_config"), client)
	realSSH, _ := exec.LookPath("ssh")
	// Writing "send" or "receive" to the sabotage file cuts that helper stream short
	// after 1 KiB. The request's op is always its first JSON field.
	sabotage := filepath.Join(d, "sabotage")
	put(t, filepath.Join(bin, "ssh"), fmt.Sprintf(`#!/bin/sh
ssh() { '%s' -F '%s' "$@"; }
case "$*" in *_helper) ;; *) ssh "$@"; exit $?;; esac
# Run the helper built for this test even when the login shell's PATH finds an installed one.
for a; do shift; [ "$a" = "agent-session-archiver _helper" ] && a="'%s' _helper"; set -- "$@" "$a"; done
prefix=$(dd bs=1 count=16 2>/dev/null)
case "$(cat '%s' 2>/dev/null):$prefix" in
send:'{"op":"send",'*) { printf %%s "$prefix"; cat; } | ssh "$@" | head -c 1024; exit 1;;
receive:'{"op":"receive"'*) { printf %%s "$prefix"; head -c 1024; } | ssh "$@"; exit $?;;
esac
{ printf %%s "$prefix"; cat; } | ssh "$@"
`, realSSH, filepath.Join(d, "ssh_config"), filepath.Join(bin, "agent-session-archiver"), sabotage))
	os.Chmod(filepath.Join(bin, "ssh"), 0700)
	put(t, filepath.Join(bin, "rsync"), "#!/bin/sh\necho 'rsync must not be used' >&2\nexit 1\n")
	os.Chmod(filepath.Join(bin, "rsync"), 0700)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	// Paths deliberately contain characters that must not become shell syntax.
	source := filepath.Join(d, "source ' $(nope)")
	put(t, filepath.Join(source, "projects/p/session.jsonl"), "{\"cwd\":\"/project\",\"sessionId\":\"s\"}\n")
	h := Host{SSH: "fixture", User: "u", Host: "h", Harnesses: map[string]Harness{"claude": {Directories: []string{source}}}}
	home, e := os.UserHomeDir()
	if e != nil {
		t.Fatal(e)
	}
	extra, e := os.MkdirTemp(home, "asa-ssh-fixture-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(extra) })
	put(t, filepath.Join(extra, "projects/p/session.jsonl"), "{}\n")
	h.Harnesses["claude"] = Harness{Directories: []string{source, "~/" + filepath.Base(extra)}}
	root := filepath.Join(d, "archive ' $(nope)")
	r, e := Pull(context.Background(), h, root, false)
	if e != nil || r.Selected != 2 {
		t.Fatalf("pull %+v: %v", r, e)
	}
	r, e = Pull(context.Background(), h, root, false)
	if e != nil || r.Selected != 0 {
		t.Fatalf("unchanged pull %+v: %v", r, e)
	}
	r, e = Push(context.Background(), h, "fixture:"+root, false)
	if e != nil || r.Selected != 0 {
		t.Fatalf("switch direction %+v: %v", r, e)
	}
	f, _ := os.OpenFile(filepath.Join(source, "projects/p/session.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("{\"type\":\"user\"}\n")
	f.Close()
	r, e = Push(context.Background(), h, "fixture:"+root, false)
	if e != nil || r.Selected != 1 {
		t.Fatalf("append push %+v: %v", r, e)
	}
	if e = Verify(root); e != nil {
		t.Fatal(e)
	}
	other := filepath.Join(d, "other")
	if _, e = Push(context.Background(), h, "fixture:"+other, false); e != nil {
		t.Fatal(e)
	}
	a, _ := LoadManifests(root)
	b, _ := LoadManifests(other)
	for id, m := range a {
		if b[id].Members[0].SHA256 != m.Members[0].SHA256 {
			t.Fatal("push/pull bytes differ")
		}
	}
	// macOS sshd overrides TMPDIR, so ask the source where it would stage files.
	remoteTmp, e := exec.Command("ssh", "fixture", `printf %s "${TMPDIR:-/tmp}"`).Output()
	if e != nil {
		t.Fatal(e)
	}
	staged := func() map[string]bool {
		found, _ := filepath.Glob(filepath.Join(string(remoteTmp), "agent-session-archiver-*"))
		set := map[string]bool{}
		for _, p := range found {
			set[p] = true
		}
		return set
	}
	before := staged()
	// Interrupt each direction mid-stream; neither may publish or leave source copies.
	f, _ = os.OpenFile(filepath.Join(source, "projects/p/session.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("{\"type\":\"assistant\",\"padding\":\"" + strings.Repeat("x", 4096) + "\"}\n")
	f.Close()
	put(t, sabotage, "send")
	if _, e = Pull(context.Background(), h, root, false); e == nil || !strings.Contains(e.Error(), "incoming retained") {
		t.Fatalf("interrupted pull: %v", e)
	}
	put(t, sabotage, "receive")
	if _, e = Push(context.Background(), h, "fixture:"+other, false); e == nil || !strings.Contains(e.Error(), "incoming retained") {
		t.Fatalf("interrupted push: %v", e)
	}
	os.Remove(sabotage)
	for _, archive := range []string{root, other} {
		if e = Verify(archive); e != nil {
			t.Fatal(e)
		}
	}
	for p := range staged() {
		if !before[p] {
			t.Fatalf("transfer staged a copy on the source: %s", p)
		}
	}
	if r, e = Push(context.Background(), h, "fixture:"+other, false); e != nil || r.Selected != 1 {
		t.Fatalf("push after interruption %+v: %v", r, e)
	}
	if _, e = Pull(context.Background(), h, root, false); e != nil {
		t.Fatal(e)
	}
	offline := h
	offline.SSH = "offline"
	offline.Host = "offline"
	reports := PullHosts(context.Background(), []Host{offline, h}, root, false)
	if len(reports) != 2 || reports[0].Err == nil || reports[1].Err != nil {
		t.Fatalf("%+v", reports)
	}
}
func TestRemoteArgumentValidation(t *testing.T) {
	for _, dest := range []string{"-oProxyCommand=evil:/tmp/a", "host:relative", "host:/tmp/a\nwrong", "host;evil:/tmp/a"} {
		if _, _, e := destination(dest); e == nil {
			t.Fatalf("accepted %q", dest)
		}
	}
}
func TestExchangeRejectsOutputAfterConsumer(t *testing.T) {
	bin := t.TempDir()
	// A helper that keeps writing after the reply its caller expects.
	put(t, filepath.Join(bin, "ssh"), "#!/bin/sh\ncat >/dev/null\nexec head -c 1048576 /dev/zero\n")
	os.Chmod(filepath.Join(bin, "ssh"), 0700)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	done := make(chan error, 1)
	go func() {
		done <- exchange(context.Background(), "fixture", request{Op: "send"}, nil, func(r io.Reader) error {
			_, e := r.Read(make([]byte, 1))
			return e
		})
	}()
	select {
	case e := <-done:
		if e == nil || !strings.Contains(e.Error(), "unexpected output") {
			t.Fatalf("trailing output: %v", e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exchange hung on trailing output")
	}
}
