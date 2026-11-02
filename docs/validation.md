# Implementation validation

The implementation uses ordinary files, SSH and rsync. SQLite is only an optional
source-side dependency for OpenCode. Public fixtures are synthetic; host configuration,
transcripts and rollout logs remain private.

## Automated evidence

- Go race tests and vet pass, including real loopback SSH push/pull, interrupted
  transfer, offline-host continuation and switching transfer direction.
- Linux and macOS CI pass. The Linux job includes real SSH integration.
- A 100,000-trace SSD fixture measured discovery at 4.7 seconds, manifest scanning
  at 2.4 seconds and unchanged selection at 55 milliseconds. One changed trace was
  selected. This does not measure HDD ingestion or durability-flush performance.
- An isolated AgentsView import reads one retained synthetic Claude session, with
  Hermes excluded, and leaves the archive verifiable.
- An independent review found three recovery/concurrency problems. Tests reproduced
  each before fixes: recovery of complete unacknowledged transfers, recovery isolation
  between hosts, and retaining first placement across concurrent initial captures.
  No minor review findings were deferred.

## Implementation decisions and limits

- An ignored manual worktree kept implementation separate from an unrelated attached
  repository; the app did not manage that worktree.
- Codex's shared index is one metadata trace per source root. Consumers must distinguish
  that metadata record from a conversation.
- Independent review preceded live ingestion to catch preservation problems first;
  it did not establish actual-host coverage or final rollout counts.
- Viewer compatibility is proven only for the Claude import fixture. Other native
  layouts and historical database aggregation can require adapters.
- Process-interruption recovery is tested. Physical power loss has not been injected;
  archive publication uses file and directory synchronization plus rename.
- Repeated Claude companions would otherwise consume about 121 GB for 6.9 GB of
  unique source bytes in the measured inventory. Hard links share stable tool-owned
  copies, with copy fallback and rename-based updates. They never link live sources.
  Editing archive files in place can affect other linked paths: restore by copying.
- HDD validation in hash-ID order caused scattered reads across projects. Discovery,
  batch checking/publication and verification now follow archive-path order. The
  existing behavioral checks still pass; physical layout can still limit throughput.
- Disposable source staging does not flush each file to durable storage. This removes
  measured journal waits; source power loss before transfer requires recapture.
  Destination validation and durability barriers remain enabled.

Native harness homes are supported. Arbitrarily rearranged historical backups are
not automatically discovered, and raw capture does not promise every viewer will
parse every retained transcript. No remote hosts or periodic schedule are enrolled
implicitly. Capture frequency determines the loss window before a file reaches the
archive. Database snapshots accumulate indefinitely.
