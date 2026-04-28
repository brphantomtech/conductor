// Package docstore is the Doc Store Manager (SPEC §10). It allows
// project documentation to live outside the codebase repository — in
// local_fs, git_repo, s3, notion, confluence, or custom backends — and
// indexes those documents into the Knowledge Engine alongside the codebase
// for unified RAG. It also resolves docs:// URI references to HARNESS.md.
//
// Tier 2 (domain engine). Imports Tier 0 + Tier 1.
package docstore
