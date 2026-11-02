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
