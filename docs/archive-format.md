# Archive format

Version 1 stores traces under user/host/project/harness/trace-id, with trace.json
and native files under raw/. User/host are explicit safe path components. Project
names combine a bounded readable prefix and a hash. Trace IDs hash user, host,
harness, normalized source root and native ID; separate installations remain distinct.
Existing manifests fix a trace's project placement after first capture.

Manifests retain source paths, working directory, source sizes/mtimes and captured
SHA-256 hashes. Source mtime is a change hint, not an integrity proof. The archive
never infers deletions from missing sources. Source root relocation creates a new
identity; Codex movement between sessions and archived_sessions does not.

OpenCode database captures use _sources/opencode/<source-id>/<snapshot-hash>/source.json
and raw/opencode.db within each user/host directory. Session trace manifests reference
the snapshot hash and native session ID. Each distinct database snapshot is retained
once; older snapshots are never automatically removed. Source manifests are verified
alongside session manifests, and missing references fail verification.
