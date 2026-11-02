# Agent session archiver: simple design

## What it does

Collect original agent traces from configured machines into an ordinary-file archive.
A source-directory wipe must not delete anything already archived. AgentsView and
other viewers consume the archive; they do not own it.

One Go executable uses system SSH and rsync. sqlite3 is an optional dependency only
for database-backed harnesses. No archive database, custom transport protocol,
transaction journal, service daemon, or plugin framework. Cron/systemd can schedule it.

## Configuration and commands

Use the same host records for push and pull:

```toml
archive = "/srv/agent-traces"

[[hosts]]
ssh = "workstation"
user = "jesse"
host = "workstation"

[hosts.harnesses.claude]
directories = ["~/.claude", "~/.claude-work", "~/.claude-personal"]

[hosts.harnesses.codex]

[[hosts]]
ssh = "laptop"
user = "jesse"
host = "laptop"

[hosts.harnesses.claude]
```

- `pull --all`: sequentially fetch every configured host into `archive`.
- `pull laptop`: fetch the record selected by its unique `host` name.
- `push laptop archive-server:/srv/agent-traces`: collect locally using the laptop
  record and send to that destination. Its `ssh` field is unused in push mode.
- `verify`: check the local configured archive without contacting source hosts.
- `--config FILE` selects configuration; `--full` on push/pull bypasses size/mtime
  shortcuts. There is no separate reconcile command.

A harness table enables collection. Missing tables exclude the harness. Omitted
`directories` uses native defaults; a nonempty list replaces them. Empty lists are
errors. Directories are harness data roots: Claude roots contain projects/, Codex
roots contain sessions/ and archived_sessions/. Document roots for every adapter.
Expand ~ on the source under its executing account, not on the controller. Reject
other relative roots and duplicate normalized roots. Do not expand shell expressions.

`user` and `host` select stable archive directories; `ssh` selects the connection.
SSH config supplies credentials, ports and jump hosts. Continue after a host fails,
report each failure, and return nonzero if any requested collection was incomplete.
The enrollment config lives outside this public repository.

## Files on disk

    <archive>/<user>/<host>/<project>/<harness>/<trace-id>/
        trace.json
        raw/<native files and relative subdirectories>

No sharding. Each trace has ordinary native files and a small JSON manifest with
source root/path, native ID, original working directory, project identity, and each
member's size and hash. Multiple installations of a harness share the project/harness
grouping but retain separate source identities when IDs collide. Never deduplicate
independent installations merely because native IDs match.

Group worktrees/checkouts by normalized Git remote, including remote host and owner.
Prefer origin; use Git's common directory for a local repository without a remote.
If the repository is gone or ambiguous, use recorded evidence or the original working
path. Nonrepository names replace path delimiters and include a collision suffix.
Bound and sanitize all archive path components.

Choose the project on first capture and retain that placement. Do not automatically
move old traces when remotes change or more metadata appears. Match existing traces
by stable source identity from manifests; an in-memory map rebuilt during discovery
is sufficient initially. No persistent database or automatic migration subsystem. Identity is user, host,
harness, normalized absolute source root, and native session ID (with harness-specific
scope if IDs are only locally unique). Without a native ID, use source-relative path.
Moving a Codex session between sessions/ and archived_sessions/ retains its identity;
moving an installation root creates a separate source. No migration mechanism.

## Three operations

1. **Discover:** find native traces and known companions; determine identities and
   paths. Parse only metadata needed for collection. Do not reconstruct fork ancestry,
   join sessions, or follow arbitrary paths mentioned in messages. Preserve malformed
   viewer content, compressed Codex files and companions without normalization.
2. **Transfer:** select new/changed files, prepare stable copies of those files only,
   and rsync them into destination staging over SSH. Never transfer directly over
   committed archive files. Reuse the same operations for push and pull; all SSH
   connections originate on the initiating machine, with no reverse credentials.
3. **Incorporate:** hold a per-host writer lock, validate staging and publish files
   through temporary files and rename. Publish the manifest after its files.

Support Claude, Codex, Pi, Evener and OpenCode. The initial deployment excludes Hermes.
Copy known companion files alongside the trace; accept modest duplication instead
of introducing a shared-object system. Record relationships already present in source
metadata when useful, but do not resolve a global session graph.

For append-only transcripts, existing archived bytes must be a prefix of incoming
bytes. Reject shrinkage or disagreement without replacing the archive. Source absence
never deletes archive members. Adapters explicitly identify mutable companions.

There is no atomic snapshot across independently changing files. Capture a bounded
length for a live append-only file; collect later appends next time. Open once, copy
exactly the initially observed length from that descriptor, reject short reads, and
hash the copied bytes. Do not reopen the pathname mid-capture. Check mutable-file
metadata before and after copying and reject unstable reads. This detects ordinary
concurrent changes; it is not a snapshot guarantee against adversarial file rewrites.

An interrupted incorporation can leave intact newer files with an older manifest.
Keep intended manifests and their hashes durably in destination staging until
incorporation completes. Replay a fully transferred pending batch before new discovery
or unchanged-file skipping, even if the source is now absent. Retry verifies those
bytes and repairs metadata; no rollback journal. Readers should
import after collection completes. Keep failed staging for diagnosis, report its
location, and clean successful batches. Never mark a partial operation successful.

The source produces a metadata inventory; the archive compares it with its manifests
and returns the requested file list. Fixed helper commands exchange these ordinary
JSON documents, without a persistent source catalog. The archive records source
size/mtime hints only after successful incorporation, separately from archive file
metadata. Push and pull use this same comparison. If identity cannot be recovered
after a source root moves, preserve a separate capture rather than guessing a merge.

Normal runs may walk trees and read manifests. Size/mtime are change hints; --full
reads all candidates and verifies content, catching rewrites those hints miss.
Initially transfer changed files in full; do not promise byte-delta resume or zero
metadata reads. Checksums describe captured bytes, not a parsed transcript.

Stable source copies cost temporary space proportional to changed files. Keep this
simple baseline. Direct live-file transfer is a possible future optimization, not
a task or requirement for the first implementation.

## Database exception

OpenCode stores several sessions in one database. Use sqlite3 online backup to capture
committed WAL data consistently. Store each distinct snapshot once under:

    <archive>/<user>/<host>/_sources/opencode/<source-id>/<snapshot-hash>/
        source.json
        raw/opencode.db

Trace manifests identify the native session and snapshot. Retain old snapshots because
a later database may omit deleted sessions; deduplicate identical snapshot bytes.
No automatic pruning. This approved exception can consume substantial space and must
be reported/documented. It does not require an embedded SQLite driver or archive catalog.

## What remains necessary

Restrictive permissions, SSH host verification, safe command arguments, path validation,
and refusing symlinks that escape source/archive roots. Both endpoints need the tool
and rsync; only sources with database harnesses need sqlite3. Offline roots and partial
discovery are errors, not evidence that archived content should disappear.

Test real push/pull, multiple roots, collisions, append/conflict handling, interruption,
source deletion, and WAL snapshots. Measure 100,000 traces before adding indexing or
sharding. Prove one small AgentsView import separately; do not make a general importer
part of the archiver. Raw capture completeness and viewer completeness are distinct.
Server backups and automatic remote installation remain outside scope.

## Why this approach

Separate push/pull implementations duplicate logic. Reverse pushes add connectivity
requirements. A custom protocol duplicates rsync; a catalog and session graph solve
problems not yet measured. The chosen implementation is a harness-aware collector,
existing transport tools, and a conservative destination writer.

AgentsView v0.46.1 provider research established the need for companion files, distinct
source/session identities and online SQLite backup. Its viewer-specific normalization
and private archive format are not requirements for this tool.
