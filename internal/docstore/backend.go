package docstore

import (
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
)

// Backend kinds supported by the Doc Store Manager (SPEC §10.2). Notion and
// Confluence are deferred; custom is the Phase 18 plugin seam.
const (
	BackendLocalFS = "local_fs"
	BackendGitRepo = "git_repo"
	BackendS3      = "s3"
)

// buildBackend constructs the backend a store config declares when it can be
// built without injected dependencies. local_fs is fully self-contained;
// git_repo and s3 return (nil, nil) so the caller can supply an injected
// backend via WithBackend (no live network in CI). An unknown backend is an
// error.
func buildBackend(cfg config.DocStoreConfig) (Backend, error) {
	switch cfg.Backend {
	case BackendLocalFS, "":
		return newLocalFSBackend(cfg.ID, cfg.PathOrURL, cfg.IncludePatterns, cfg.Tags), nil
	case BackendGitRepo, BackendS3:
		return nil, nil
	default:
		return nil, fmt.Errorf("docstore: unsupported backend %q for store %q", cfg.Backend, cfg.ID)
	}
}

// NewGitRepoBackend constructs a git_repo backend with an injected GitRunner so
// the start command (and tests) can wire a backend the manager cannot build on
// its own. workDir is the local clone directory.
func NewGitRepoBackend(cfg config.DocStoreConfig, workDir string, runner GitRunner) Backend {
	return newGitRepoBackend(cfg.ID, cfg.PathOrURL, workDir, cfg.Auth, cfg.IncludePatterns, cfg.Tags, runner)
}

// NewS3Backend constructs an s3 backend with an injected S3Client.
func NewS3Backend(cfg config.DocStoreConfig, client S3Client) Backend {
	return newS3Backend(cfg.ID, cfg.PathOrURL, cfg.IncludePatterns, cfg.Tags, client)
}

// NewLocalFSBackend constructs a local_fs backend (exported for CLI/test use).
func NewLocalFSBackend(cfg config.DocStoreConfig) Backend {
	return newLocalFSBackend(cfg.ID, cfg.PathOrURL, cfg.IncludePatterns, cfg.Tags)
}
