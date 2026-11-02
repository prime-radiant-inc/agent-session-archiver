# Agent session archiver

Repository: prime-radiant-inc/agent-session-archiver.

## Purpose and agreed scope

Preserve original agent traces from multiple hosts as ordinary files on an archive
server, independently of AgentsView. Deleting a source harness directory must never
delete successfully archived data. Keep raw bytes even if a viewer cannot parse them.

One Go CLI supports push and pull over SSH. Use system SSH and rsync for transport.
No custom transfer protocol, archive database, database inventory, transaction journal,
namespace authorization service, or automatic remote installation. SQLite is used
only to capture harnesses whose native storage already uses it, through the optional
sqlite3 command-line tool. Scheduling belongs to cron or systemd initially.

## Approaches and simplification

Separate push/pull implementations duplicate collection and retention logic. A pull
that asks the source to open a reverse connection requires extra credentials and
reachability. Instead, both modes share collection and incorporation code, with all
SSH connections initiated from the machine running the command.

The earlier custom wire protocol and SQLite catalog were unnecessary infrastructure.
Use SSH commands for preparation/incorporation and rsync over SSH for bytes. Multiple
connections in the same direction are acceptable. Filesystem metadata and small JSON
manifests are sufficient initially; measure actual scans before introducing a cache.

## Configuration

The archive server keeps a TOML configuration listing hosts and their harnesses:

```toml
archive = "/srv/agent-traces"

[[hosts]]
ssh = "workstation"
user = "jesse"
host = "workstation"

[hosts.harnesses.claude]
directories = ["~/.claude", "~/.claude-work", "~/.claude-personal"]

[hosts.harnesses.codex]

[hosts.harnesses.evener]

[[hosts]]
ssh = "laptop"
user = "jesse"
host = "laptop"

[hosts.harnesses.claude]
```

`ssh` is an SSH-config destination. `user` and `host` are stable archive identities;
SSH alias changes do not rename archived data. Select a configured host by `host`;
require host selectors to be unique within a configuration.

A harness table enables that harness. Unlisted harnesses are excluded. Omitted
`directories` uses native defaults; an explicit nonempty list replaces the defaults.
Reject empty lists to avoid ambiguous behavior. Each entry is a harness data root:
Claude roots contain projects/, Codex roots contain sessions/ and archived_sessions/.
Document the equivalent root for each other adapter. Resolve ~ on the source host
under its SSH account; reject other relative roots rather than depending on remote cwd.
Expand only ~ or ~/ prefixes, not shell expressions or environment substitutions.

Multiple roots of a harness feed the same project/harness grouping. Preserve the
original root and path in every trace manifest. Scope trace identity by source root
where needed to prevent native-ID collisions; never silently merge different sources.
Duplicate configured roots after normalization are an error.

Commands: `pull --all`, `pull <host>`, and `push <ssh-destination>:<archive-path>`.
All accept `--config <path>`. Pull configuration uses `archive` and `hosts` above.
Push configuration uses a `[source]` table with `user`, `host`, and nested `harnesses`
tables following exactly the same directory rules; its archive destination is the
command argument. Host lists run sequentially. Report an offline/failed host and
continue; return nonzero if any requested host fails. Credentials and connection
options stay in normal SSH configuration. Enrollment config never goes in this repo.

## Archive layout

    <archive>/<user>/<host>/<project>/<harness>/<trace-id>/
        trace.json
        raw/<native files and relative subdirectories>

No sharding. Group Git worktrees and checkouts by normalized repository remote,
including hosting service and owner, not just basename. Prefer origin or an explicit
override; do not guess among ambiguous remotes. Without a remote, use the Git common
directory to group local worktrees. For nonrepositories use the full working path
with separators replaced and a collision suffix. Keep original paths in metadata.
Resolve project metadata on the source; missing historical repositories can use
recorded metadata or an explicit unresolved group without blocking capture.

Bound and sanitize path components, preserving collision resistance. Trace identities
are stable and independent of content or physical archive location. trace.json holds
source provenance, native ID, project evidence, relationships where known, member
paths, sizes and hashes. Keep native companions together; copying a small shared
companion is preferable to a general-purpose reference/object-storage system.

## Collection and incorporation

Adapters discover known transcript roots and companions, not entire home directories
or paths mentioned inside messages. Do not follow symlinks outside configured roots.
Support Claude, Codex, Pi, Evener and OpenCode initially; the first deployment excludes
Hermes. Preserve compressed Codex sources unchanged. Metadata extraction failure does
not prevent preserving a recognizable raw source. Do not reproduce viewer-specific
splitting or joining of logical sessions.

Prepare a stable outgoing batch using temporary files, recording observed lengths
and hashes. An active append can wait for the next run. Transfer into a separate
incoming staging directory, never directly over the permanent archive. Use rsync
without deletion propagation and without in-place writes to archived files.

An archive-side incorporation step holds a per-host lock, validates paths and hashes,
and copies verified files through temporary files and rename. Append-only transcripts
may advance only if archived bytes are a prefix of incoming bytes. Shrinkage or prefix
disagreement preserves the old archive and reports a conflict. Source absence never
removes an archive member. Mutable companions are explicitly identified by adapters.

Do not promise an atomic snapshot of multiple independently changing source files.
Publish a manifest after its files. Interrupted incorporation may leave intact newer
files with older metadata; a retry verifies those bytes and repairs the manifest.
Never claim batch success before incorporation completes. Keep failed staging for
retry/diagnosis; clean successful temporary batches. This requires no transaction
journal or whole-archive rollback mechanism. Archive readers should avoid importing
a host while its incorporation lock is held.

Normal runs may stat source trees and manifests; do not demand zero metadata reads.
Skip unchanged payloads using recorded size/mtime hints, and verify changed copies.
An explicit full verification/reconciliation detects same-size/same-mtime rewrites.
Measure 100,000-trace discovery before adding an inventory database or sharding.

## Database-backed harnesses

For OpenCode, use sqlite3's online backup facility to create a consistent snapshot,
including committed WAL data; never copy only a live main database file. Require the
sqlite3 CLI only when such a harness is enabled. No embedded Go SQLite driver.

Store each distinct snapshot once under:

    <archive>/<user>/<host>/_sources/opencode/<source-id>/<snapshot-hash>/
        source.json
        raw/opencode.db

Trace manifests identify their source snapshot and native session ID. This is a
specific exception for shared databases, not a generic object store. Retain older
snapshots because later databases can omit deleted sessions. Deduplicate identical
snapshot bytes; no automatic pruning initially. Document potentially significant
storage growth. Jesse approved this retention exception for database-backed harnesses.

## Transport and access

Push prepares locally, rsyncs to remote staging, and invokes remote incorporation.
Pull invokes remote preparation, rsyncs to local staging, and incorporates locally.
The source never needs reverse credentials. Both endpoints need the CLI and rsync.
Use SSH accounts and filesystem permissions for access; no new authorization layer.
Use restrictive data permissions and preserve normal SSH host-key verification.
Never interpolate discovered paths into shell commands; pass structured configuration
to fixed helper commands. Validate host/destination arguments and quote remote shell
arguments correctly when invoking rsync. Staging paths must not escape their roots.

## AgentsView and validation

The archive is the deliverable; AgentsView is a consumer. Prove a small disposable
native-layout import on synthetic fixtures before building any broad view-generation
subsystem. Report unsupported import shapes, including historical shared snapshots,
without claiming complete indexing. Viewer failures never prevent raw preservation.

Test repeated runs, appends, truncation, interrupted transfers/incorporation, source
deletion, name collisions, multiple harness roots, companions and malformed content.
Exercise both push and pull over real SSH and SQLite capture with a live WAL writer.
Measure a 100,000-trace fixture. Verify archive bytes independently of viewer counts.
Roll out locally first, then enroll specifically chosen remote hosts. Separate archive
server backups remain outside this tool's scope.

## Research

AgentsView v0.46.1 provider code distinguishes physical captures from logical sessions,
includes Claude/Codex/Evener companion files, and uses online backup for database
sources. Its project identity code separates repository identity from worktrees.
These findings inform adapters; the archive does not depend on its private format.
