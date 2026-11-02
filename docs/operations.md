# Operations

Implementation is in progress; commands are not yet released.

Each configured harness root contains:

| Harness | Data beneath each root |
| --- | --- |
| Claude | projects/, including subagents and per-session tool-results |
| Codex | sessions/, archived_sessions/, optional session_index.jsonl |
| Pi | sessions/ |
| Evener | projects/*/sessions/; project worktrees are not traversed |
| OpenCode | opencode.db (database capture requires sqlite3) |

Explicit roots replace defaults. Home expansion happens on the source. Only listed
harnesses are enabled. Duplicate roots are rejected; root symlinks are allowed, but
symlinks within roots are refused. Malformed message content is preserved.

Changed files are copied to source staging before transfer, so temporary free space
must accommodate changed payloads. Copies use one descriptor, a bounded length, and
checksums. Append-only growth after the bounded read is picked up next time. Mutable
companions require stable metadata across the read. --full bypasses metadata hints.

Codex's shared session index is captured once per root as a metadata trace under
`shared codex metadata`, rather than duplicated for every session.

## Incorporation and recovery

Destination batches live under .incoming/batch-*. A ready.json contains the durable
intended manifests only after every captured or reused member verifies. Completed
transfers are replayed before discovery, even when the original sources disappeared.
A per-host advisory lock rejects overlapping writers. Raw files publish through
synced temporary files and rename; trace.json follows. Readers should run after sync.

Conflicting transcripts are retained unchanged and the command fails. Failed staging
is retained for diagnosis; successful batches are removed. Do not edit ready.json.
To discard an unrecoverable conflicting batch, inspect it and remove only its named
.incoming/batch-* directory; never delete archive trace directories to clear a conflict.
Verification checks hashes/sizes without needing source directories.

## SSH commands

Build with `go build -o bin/agent-session-archiver ./cmd/agent-session-archiver`.
Install this executable as `agent-session-archiver` on the noninteractive SSH PATH
of both endpoints. Both need rsync 3.x (including on macOS); source-side SQLite is
needed only for OpenCode. Normal SSH configuration and host-key checking apply.

```
agent-session-archiver --config config.toml pull --all
agent-session-archiver --config config.toml pull laptop
agent-session-archiver --config config.toml push laptop archive:/srv/agent-traces
agent-session-archiver --config config.toml verify
```

The default config path is ~/.config/agent-session-archiver/config.toml. Flags may
appear before or after commands. Archive paths are absolute. Destination connections
use SSH aliases/hostnames; configure IPv6 addresses through an SSH alias. Push reads
the selected host record locally and ignores its ssh field. --full forces content
capture despite unchanged metadata. Exit status is 0 for success, 1 for operational
failure (including partial collection), 2 for invalid arguments/configuration.

Pull --all runs sequentially and continues after a failed host. Reported selected
counts are traces requiring transfer, not a promise of successful incorporation;
check exit status. Source batches use the source process's TMPDIR and are removed
only after success. Interrupted transfers leave incoming staging for inspection;
only ready batches are replayed, so incomplete network transfers never publish.

## Development checks

Run `go test -race ./...` and `go vet ./...`. The real SSH integration gate is
`ASA_SSH_TEST=1 go test ./...`; it requires ssh, sshd, ssh-keygen and rsync. It uses
a disposable loopback server, temporary keys and strict host verification. Without
the variable the SSH test explicitly skips. No real transcripts are test fixtures.

## OpenCode

The adapter uses the sqlite3 CLI's online backup command and queries the resulting
snapshot, not a changing live session table. It currently recognizes the native
`session(id, directory, ...)` table. If that schema is unsupported, the raw snapshot
still transfers and the run reports incomplete session discovery. No SQLite library
is linked into the archiver. A distinct snapshot stores the full database, so frequent
collection of a busy database can consume substantial space. Choose its schedule
accordingly; identical snapshot bytes are deduplicated and no snapshots are pruned.
