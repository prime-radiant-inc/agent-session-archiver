# agent-session-archiver

Preserve original agent session traces as ordinary files on an archive server,
independently of AgentsView or other viewers.

**Status: design and implementation planning. No usable CLI is shipped yet.**

The approved design provides one CLI with SSH push and pull, archive organization
by user / host / repository or path / harness / trace, and retention independent
of source deletion. Shared database snapshots are retained once per distinct capture.

- [Design](docs/superpowers/specs/2026-11-01-archive-design.md)
- [Implementation plan](docs/superpowers/plans/2026-11-01-archive.md)

Do not commit real transcripts, credentials, host enrollment configuration, or
archive data to this repository. Tests must use synthetic fixtures.
