# Implementation validation

The implementation uses ordinary files, SSH and rsync. SQLite is only an optional
source-side dependency for OpenCode. Public fixtures are synthetic; host configuration,
transcripts and rollout logs remain private.

## Automated evidence

- Go race tests and vet pass, including real loopback SSH push/pull, interrupted
  transfer, offline-host continuation and switching transfer direction.
- Linux and macOS CI pass. The Linux job includes real SSH integration.
- A 100,000-trace SSD fixture measured discovery at 5.8 seconds, manifest scanning
  at 3.2 seconds and unchanged selection at 59 milliseconds. One changed trace was
  selected. This does not measure HDD ingestion or durability-flush performance.
- An isolated AgentsView import reads one retained synthetic Claude session, with
  Hermes excluded, and leaves the archive verifiable.
- An independent review found three recovery/concurrency problems. Tests reproduced
  each before fixes: recovery of complete unacknowledged transfers, recovery isolation
  between hosts, and retaining first placement across concurrent initial captures.
  No minor review findings were deferred.

## HDD observation

The first staged-content check on the local spinning disk took 46.8 minutes for
47,025 records containing about 155 GB of logical file content, including shared
companions. This excludes transfer, publication and final archive verification;
it is an observation from the live rollout, not a controlled benchmark. Small-file
and directory I/O make this workload substantially slower than the SSD metadata
fixture. Plan the initial ingestion accordingly; ordinary incremental captures
select changed traces instead of transferring the whole corpus.

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

Native harness homes and explicitly configured historical session trees are supported.
Arbitrarily rearranged backups are not automatically discovered, and raw capture does not promise every viewer will
parse every retained transcript. No remote hosts or periodic schedule are enrolled
implicitly. Capture frequency determines the loss window before a file reaches the
archive. Database snapshots accumulate indefinitely.


## Live rollout status

The initial local capture contains 47,025 records. Its first publication was stopped
when corpus inspection showed recorded repository metadata was being ignored for
removed worktrees. Regression tests now cover Codex and Evener recorded origins.
Reclassifying 25,261 captured records changes only their project field, reducing
project/path groups from 26,914 to 10,278; captured identities and member hashes are
unchanged. Original sources and the prior partial publication remain retained.
This is a one-off correction of an unfinished deployment, not an archive migration
feature. Existing committed placement remains stable in normal operation.

Historical tree collection passed the race, real SSH and isolated viewer tests.
The expanded local inventory finds 75,402 records, including 28,167 Superpowers
records and 136 historical Evener records. The two explicitly requested remote
hosts were inventoried and enrolled over freshly authenticated unattended SSH.
An hourly, nonoverlapping systemd service is enabled and its first run is active.
It runs a full archive verification after the first successful sync of all enrolled
hosts, then checks that every original capture identity remains present. These
inventory counts are not completed-transfer counts; final rollout verification
is outstanding. Renaming original historical directories remains explicitly paused.

## M5Max additions

Lace and Grok adapters use the existing ordinary-file capture and retention path.
Real-file regressions cover Lace filename collisions and source deletion, and Grok
whole-session capture, companion refresh, append, shrink rejection and source
deletion. Both failed for unsupported harnesses before implementation. The full
race/SSH/viewer suite and vet pass after the additions.

M5Max's historical stores exposed mixed Claude/Codex conversation archives. A
regression reproduced three discovered traces instead of two; Claude archive
collection now excludes Codex session headers, and Codex year subtrees are enrolled
separately. The initial deployment had already published 177 such records under
Claude from one remote host. Those task-created copies were preserved separately
before scheduling correct Codex capture. Original remote files were not changed.
Do not interpret earlier historical Claude discovery totals as final classified
session counts. Bulk rollout and its final verification remain outstanding.

The adapter review found no Critical or Important issues. Its two Minor notes were
addressed by asserting actual refreshed Grok summary bytes and correcting the
project-discovery overview. The private native M5Max inventory discovers 27,165
records without errors; this remains discovery evidence, not completed transfer.

Actual cached OpenCode backups on macOS exposed an Apple SQLite failure opening a
standalone WAL-mode backup read-only without sidecar files. A real SQLite regression
first failed because enumeration created WAL/SHM sidecars. Completed backups now use
an immutable read-only URI; the live source still uses SQLite online backup. The
race/SSH/viewer suite and vet pass, and all three affected real cached databases now
enumerate their sessions successfully on M5Max. Only the already-completed private
backup is opened immutable; a changing live database must never use that mode.
