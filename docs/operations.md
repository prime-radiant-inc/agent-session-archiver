# Operations

Build from source using the instructions below; no packaged release is published.

Each configured harness root contains:

| Harness | Data beneath each root |
| --- | --- |
| Claude | projects/, including subagents and per-session tool-results |
| Codex | sessions/, archived_sessions/, optional session_index.jsonl |
| Pi | sessions/ |
| Evener | projects/*/sessions/; project worktrees are not traversed |
| OpenCode | opencode.db (database capture requires sqlite3) |
| Lace | transcripts/; native JSONL files retain their relative-path identities |
| Grok | sessions/; each session directory, including metadata and attachments |

For historical trees that contain session files directly, use `archives` alongside
(or instead of) `directories`, for example:

```toml
[hosts.harnesses.claude]
directories = ["~/.claude"]
archives = ["~/.config/superpowers/conversation-archive"]
```

Archive roots are supported for Claude, Codex, Pi, Evener, Lace and Grok. Claude, Codex, Pi and Lace
walk the configured tree; Evener reads its direct session files without descending
into artifacts. Claude summary text and known companions are retained. OpenCode
backup directories use `directories` and must contain `opencode.db`. Originals stay
in place; collection never moves or marks a source. Different roots retain separate
identities, even when they hold copies of the same native session.

Explicit roots replace defaults. Home expansion happens on the source. Only listed
harnesses are enabled. Duplicate roots are rejected; root symlinks are allowed, but
file symlinks must resolve within the configured root. Directory symlinks are
reported instead of traversed. Malformed message content is preserved.

Changed files are copied to source staging before transfer, so temporary free space
must accommodate changed payloads. Copies use one descriptor, a bounded length, and
checksums. Append-only growth after the bounded read is picked up next time. Mutable
companions require stable metadata across the read. --full bypasses metadata hints.

Project discovery prefers the live Git origin/common directory. When the working
repository is unavailable, it uses Codex session metadata, Evener companion metadata or a single Grok
summary remote containing a recorded Git origin, then falls back to the working path.
Existing archive placement remains stable.

Codex's shared session index is captured once per root as a metadata trace under
`shared codex metadata`, rather than duplicated for every session.

## Incorporation and recovery

Destination batches live under .incoming/batch-*. A ready.json contains the durable
intended manifests only after every captured or reused member verifies. A complete
batch.json-only transfer can also be verified and promoted during recovery. Completed
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
recovery verifies an unacknowledged batch before replaying it, so incomplete
network transfers never publish. Recovery is scoped to the selected host; another
host's retained conflict does not block it.

## Scheduling

Run push or pull from an external scheduler such as cron, a systemd timer or launchd.
Use an absolute executable and config path, retain command output, and alert on a
nonzero exit status. Choose the interval according to acceptable loss before capture;
there is no filesystem watcher. Avoid overlapping runs for the same host. SSH must
work without an interactive prompt under the scheduler's account.

## Development checks

Run `go test -race ./...` and `go vet ./...`. The real SSH integration gate is
`ASA_SSH_TEST=1 go test ./...`; it requires ssh, sshd, ssh-keygen and rsync. It uses
a disposable loopback server, temporary keys and strict host verification. Without
the variable the SSH test explicitly skips. No real transcripts are test fixtures.

## OpenCode

The adapter uses the sqlite3 CLI's online backup command and queries the resulting
snapshot, not a changing live session table. Completed snapshots are queried as
immutable SQLite files, so enumeration needs no WAL/SHM sidecars and does not change
the backup. Live databases still use the normal online-backup connection. It currently recognizes the native
`session(id, directory, ...)` table. If that schema is unsupported, the raw snapshot
still transfers and the run reports incomplete session discovery. No SQLite library
is linked into the archiver. A distinct snapshot stores the full database, so frequent
collection of a busy database can consume substantial space. Choose its schedule
accordingly; identical snapshot bytes are deduplicated and no snapshots are pruned.

## Scale and viewer checks

`ASA_SCALE_TEST=1 go test -v ./internal/archive -run TestHundredThousandTraces`
creates 100,000 synthetic sources and real manifest directories. On the final Linux
SSD run, discovery took 5.8 s, manifest scanning 3.2 s, and unchanged selection 59 ms.
These measure metadata/discovery, not spinning-disk ingestion throughput or fsync cost.
No sharding or inventory database is required by that measurement.

`ASA_AGENTSVIEW_TEST=1 go test -v ./internal/archive -run TestAgentsViewImportProof`
uses an installed AgentsView and isolated HOME/data directories. It copies retained
Claude bytes into one native-layout view, verifies the imported session count and
rechecks the source archive. This proves that import path only; historical OpenCode
snapshot aggregation and complete multi-harness viewer imports remain separate work.

## Shared companion files

Stable copies inside a batch and identical files incorporated by that batch can share
filesystem hard links. rsync preserves those links. Live source files are never
hard-linked into staging or the archive. Updates use temporary files and rename,
which detach the updated path rather than changing other traces' retained bytes.
Treat archive raw files as immutable; restore by copying before editing. Filesystems
that cannot create a link fall back to copying. This saves shared Claude tool-result
space without a separate object store or persistent index.

Source preparation uses disposable temporary copies without per-file durability
flushes. The receiver still validates transferred hashes and durably publishes the
archive. A source-side power loss before transfer requires collecting again from the
originals. After an interrupted SSH connection, a remote preparation process may
finish independently; check it has stopped before removing its outgoing batch.

An hourly systemd user timer can use `OnCalendar=hourly` with `Persistent=true`
and a `Type=oneshot` service running the absolute `pull --all` command. Set
`TimeoutStartSec=infinity` for large initial transfers. A timer does not start a
second instance while that service is active. Enable user lingering if sync must
continue after logout. Keep native paths, host enrollment and SSH credentials in
private local configuration, outside this repository. Test a fresh noninteractive
SSH connection: an existing multiplexed connection can mask an unusable unattended
key. Run a full archive verification after the initial successful sync; repeating a
full content read every hour can consume the entire interval on a spinning disk.


## Lace, Grok and mixed historical trees

Lace defaults to `~/.lace` and Grok to `~/.grok`. Both accept multiple `directories`
or direct session-tree `archives`. Lace keeps each JSONL file byte-for-byte; a
relative-path identity prevents same-named files in different directories colliding.
If the transcript records no working directory, its project remains unknown.

Grok captures each directory containing `summary.json` or `chat_history.jsonl` as
one trace, including nested attachments and terminal output. Live `.lock` files are
excluded. JSONL members are append-only; other session companions can refresh.
Project discovery uses the summary's working directory, with its single recorded
Git remote as a fallback when the repository is gone. Multiple unnamed remotes are
not guessed. Missing/malformed summary metadata can fall back to the native encoded
working-directory component. Original bytes, including malformed records, remain.

A historical directory named `conversation-archive` may contain Codex rollouts as
well as Claude files. Claude archive discovery skips records with Codex session
headers. Configure the Codex subtree (often a year directory) under Codex `archives`
as well to capture those records with the right harness and native session ID.

If a source machine holds historical copies from other hosts, preserve the original
logical `host` labels and configure the cache paths as roots. The SSH endpoint and
logical host label need not match. When those same labels already exist in a live
host configuration, use a separate configuration for the caches and run both
configurations sequentially. Different source roots keep distinct trace identities;
this does not deduplicate a live trace against its copied historical versions.

Large companion inventories can exceed hundreds of megabytes even when their raw
files share hard links. The authenticated SSH helper accepts the full structured
inventory without a fixed byte cap; both endpoints need memory proportional to that
metadata. `ASA_SCALE_TEST=1 go test ./internal/archive -run TestLargeCompanionInventoryProtocol`
exercises selection of 10,000 traces with 64 long-path companion entries each. This
checks the metadata exchange, not transfer throughput or a bounded-memory guarantee.
