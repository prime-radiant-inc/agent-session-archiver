# agent-session-archiver

Preserve original agent session traces as ordinary files on an archive server,
independently of AgentsView or other viewers.

One Go executable discovers native files, transfers changed captures with rsync over
SSH, and safely incorporates them. Push and pull use the same host configuration.
Source deletion never deletes committed traces; truncated or conflicting transcripts
are rejected. SQLite is used only through the optional sqlite3 CLI for OpenCode.

## Build and run

Requires Go 1.22+ to build, and SSH plus rsync 3.x on both endpoints. Install the
executable on each endpoint's noninteractive SSH PATH. OpenCode sources also need
sqlite3 with JSON output support.

```sh
go build -o bin/agent-session-archiver ./cmd/agent-session-archiver
agent-session-archiver --config config.toml pull --all
agent-session-archiver --config config.toml pull laptop
agent-session-archiver --config config.toml push laptop archive:/srv/agent-traces
agent-session-archiver --config config.toml verify
```

Start with [config.example.toml](config.example.toml). A harness table enables that
harness; optional directories replace its defaults. Each host can have multiple
installations of the same harness. `~` expands on the source. Add `--full` to push/pull
to bypass metadata shortcuts. Scheduling remains external to the tool.

```text
<archive>/<user>/<host>/<project>/<harness>/<trace-id>/
    trace.json
    raw/<native files>
```

Supported sources: Claude, Codex (including compressed rollouts), Pi, Evener and
OpenCode databases. No Hermes collection. Shared OpenCode snapshots are retained once
per distinct capture; this can consume substantial space for busy databases.

The archive format is independent of any viewer. AgentsView import currently requires
materializing a native-layout copy; the test suite proves one Claude import. There
is no general viewer export command or background service.

- [Operations and recovery](docs/operations.md)
- [Archive format](docs/archive-format.md)
- [Design](docs/superpowers/specs/2026-11-01-archive-design.md)
- [Implementation plan](docs/superpowers/plans/2026-11-01-archive.md)

Run `go test -race ./...` and `go vet ./...`. Enable real SSH integration with
`ASA_SSH_TEST=1 go test ./...`; the required test dependencies and optional scale and
AgentsView checks are described in the operations guide.

Do not commit real transcripts, credentials, host configuration, or archive data.
All repository test fixtures are synthetic.
