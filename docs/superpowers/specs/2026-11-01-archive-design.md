# Agent session archiver

Design approved by Jesse, 2026-11-01. Repository: prime-radiant-inc/agent-session-archiver.

## Intent and agreed decisions

Centralize original agent traces from multiple machines on an archive server,
independently of AgentsView or any other index. Deleting source .claude or .codex
directories must not delete successfully archived traces. Capture cadence can
differ by host; scheduling is external to the CLI initially.

Use ordinary files, organized by user, host, project, harness, then trace directory.
Group Git checkouts and worktrees by repository. For nonrepositories use the full
working path with separators replaced, retaining the original path in metadata.
No sharding initially, even for projects with 100,000 traces. Support push and pull
over SSH with one implementation. Harness inclusion is explicit; the initial deployment excludes Hermes.

## Approaches

Independent push and pull programs would duplicate discovery, retention, and
transfer rules. Pull invoking a reverse SSH push would additionally require
source-to-server connectivity and credentials. The accepted approach is one CLI,
with sender and receiver components: push starts SSH from source to archive;
pull starts SSH from archive to source. Bytes always move from source to archive,
over the connection the initiator opened. No listening archiver daemon.

## Proposed implementation choices

The following implementation recommendations were included in the approved design.
Use Go for a single deployable CLI on Linux and macOS. Both endpoints need the tool;
automatic installation on remote machines is outside the initial scope. Invoke the
system ssh client, honoring its normal configuration and host verification. Never
interpolate discovered paths into shell commands. Pass configuration through a
versioned, bounded protocol over stdin/stdout; reserve stderr for diagnostics.
Negotiate protocol compatibility and fail clearly when incompatible.

Expose push HOST:ARCHIVE and pull HOST --archive LOCAL_PATH. Internal send/receive
commands implement the same inventory, transfer, verification, and publication
operations in either direction. Retrying an interrupted transfer is safe; unchanged
files are skipped. The builder may choose a proven transfer primitive or framed
stream without changing the archive format or the single SSH connection rule.

## Archive layout and identity

    archive/
      <user>/<host>/<project>/<harness>/<trace-id>/
        trace.json
        raw/<native names and relative paths>
      <user>/<host>/_sources/<harness>/<source-id>/<capture-id>/
        source.json
        raw/<shared native artifacts>

Reserve underscore-prefixed names for archive metadata. Escape user components,
bound component lengths, and append stable identity suffixes where sanitizing can
collide. Path components never contain traversal or absolute paths. Do not follow
source symlinks outside explicitly configured capture roots.

Configure a stable archive user and host identity at enrollment; SSH usernames and
aliases do not determine ownership. The receiver authorizes those namespaces from
local configuration, rather than trusting a remote sender's claimed identity.
This is initially a personal archive, not a multi-tenant service.

Repository identity is the normalized canonical remote host and repository path,
without credentials, transport spelling, or .git suffix. Select origin or an
explicit override; do not guess among ambiguous remotes. Without a remote, use
the Git common directory to group local worktrees. Non-Git projects use their full
path plus a collision suffix. Resolve on the source while metadata exists; use
recorded metadata and an explicit unresolved group for historical missing paths.
Keep repository aliases explicit; changing a remote does not silently relocate
already archived traces. Different hosts retain separate copies by design.

Trace IDs are stable and independent of content and physical archive location.
Use native IDs with harness-specific scope when required; do not assume all native
IDs are globally unique. Preserve source roots, original paths, working directories,
native IDs, project evidence, parent/fork relationships, and capture timestamps.
trace.json lists raw members or shared-source references, sizes and SHA-256 hashes.
Shared references use stable source/capture identifiers, not fragile relative paths.
Readers can resolve everything from ordinary manifests without an index database.

The archive unit follows the native trace and its companions. It does not reproduce
AgentsView's splitting or joining of logical sessions. Multiple physical files can
belong to one trace, and one shared physical source can contain multiple traces.
Unknown project information does not prevent raw capture. Later classification must
preserve trace identity and references.

## Capture and retention

Never propagate source deletions. Missing roots, offline hosts and partial discovery
are reported, not interpreted as empty authoritative inventories. Capture raw bytes
even when a viewer cannot parse them. Preserve compressed Codex files as received;
compression or parser normalization is not the archive representation.

For append-only files, verify that previously archived bytes remain a prefix before
extending the archive. Source shrinkage or prefix disagreement leaves the archived
copy intact and produces an explicit conflict; no automatic transcript versioning
or destructive replacement. Pin the observed source length and verify the bytes
actually transferred; ongoing appends can be collected on the next run.

Harness adapters identify mutable metadata and structured files explicitly. They
must not apply byte-append rules to JSON arrays, metadata documents, or databases.
Publish changes from staging only after verification. Retain committed raw files
until the updated manifest is durable; readers see only committed manifests.
Serialize writers within a user/host namespace. Partial staging is resumable or
discardable and never counts as a successful capture. Do not use source hard links.

Shared SQLite databases require an online SQLite backup including committed WAL
state. Copying the live main database alone is insufficient. Approved policy:
retain each distinct consistent database capture once under _sources, with trace
manifests referencing the relevant capture. Deduplicate identical captures by hash;
do not silently replace an older capture that may contain now-deleted sessions.
This intentionally introduces snapshot retention for shared databases, an exception
to the no-version-history preference for ordinary additive transcripts. Jesse approved this exception on 2026-11-01. No automatic
garbage collection in the first release. Shared indexes and fork dependencies are
stored once where possible and referenced, rather than copied into every trace.

## Incremental operation and recovery

A disposable local catalog tracks source discovery and previously verified bytes;
it is not the archive's source of truth. Normal runs inspect changed candidates and
transfer changes without reopening every archived manifest. Periodic reconciliation
finds missed changes. Metadata checks are hints: unchanged size/mtime is not a proof
of byte identity. Full verification is an explicit operation and may be expensive.
Measure discovery, transfer, and archive listing costs before adding sharding.

The source collector includes only known harness-owned transcript companions and
managed assets. It does not crawl arbitrary paths or URLs mentioned in messages,
copy whole home directories, or gather credentials. Restrictive archive permissions
are the default. SSH secures transport; separate disk/server backups remain outside
this tool's initial responsibilities.

## Harnesses and AgentsView

Initial file adapters target Claude, Codex, Pi, and Evener, using local source
inventory and fixtures. Include known subagent/fork dependencies and Evener metadata.
OpenCode uses the approved shared SQLite capture policy. Do not import Hermes.
Any unsupported source remains reported as unsupported rather than counted archived.

AgentsView remains a consumer. Its existing discovery rules do not accept this
layout universally. Build a disposable native-layout import view from the archive
manifests and validate it against the installed version, without altering raw files
or configuring one root per trace. No assumption that arbitrary symlinks work.
Archive success and viewer import success are separate statuses. Malformed transcripts
must remain archived even when importing fails, including Evener sources with duplicate JSON object keys rejected by AgentsView
v0.46.1.

## Validation and delivery

Verify the same fixtures through both push and pull over real localhost SSH.
Cover repeated runs, appends during capture, interrupted transfers, reconnects,
source removal, conflicts, namespace/path collisions, multiple worktrees, companion
files, invalid viewer JSON, and manifest-only reconstruction of a deleted catalog.
Exercise SQLite with a live WAL writer under the approved retention policy.
Use a 100,000-trace fixture to measure discovery and changed-only synchronization.
Assert that a wiped source cannot erase archived bytes. Validate AgentsView imports
and that no Hermes sessions appear. Do not claim collection beyond enrolled hosts.

Roll out first on one local host, verify counts and bytes, then enroll explicitly
chosen remote hosts. Pick schedules per host after
measuring runtime. Existing AgentsView database migration/import is separate work.

## Research basis

Reviewed kenn-io/agentsview tag v0.46.1: internal/parser/raw_capture.go and provider.go
define physical captures independently of logical sessions; claude_provider.go,
codex_provider.go, and evener_provider.go identify companions and lineage;
db_backed_provider.go and internal/rawcapture/sqlite_snapshot.go handle shared
database snapshots; internal/export/project_identity.go separates repository and
worktree identity. These are evidence for adapter contracts, not a dependency on
AgentsView's private archive format.
