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
