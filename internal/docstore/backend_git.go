package docstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// GitRunner abstracts the git transport for the git_repo backend so tests
// inject a fake and CI makes no network call. Clone clones url into dir (a
// no-op if dir already holds a clone); Pull refreshes an existing clone. The
// auth map carries the resolved credentials ($VAR-expanded by config) and is
// never logged or written to disk by the backend.
type GitRunner interface {
	Clone(ctx context.Context, url, dir string, auth map[string]string) error
	Pull(ctx context.Context, dir string, auth map[string]string) error
}

// gitRepoBackend is the git_repo Doc Store backend (SPEC §10.2): it clones or
// pulls a Git repository through an injected GitRunner, then walks the local
// working tree exactly like local_fs.
type gitRepoBackend struct {
	storeID         string
	url             string
	workDir         string
	auth            map[string]string
	includePatterns []string
	tags            []string
	runner          GitRunner

	fs *localFSBackend
}

// newGitRepoBackend constructs a git_repo backend. workDir is the local clone
// directory; runner performs the clone/pull (injected so tests use a fake).
func newGitRepoBackend(
	storeID, url, workDir string, auth map[string]string,
	includePatterns, tags []string, runner GitRunner,
) *gitRepoBackend {
	return &gitRepoBackend{
		storeID:         storeID,
		url:             url,
		workDir:         workDir,
		auth:            auth,
		includePatterns: includePatterns,
		tags:            tags,
		runner:          runner,
		fs:              newLocalFSBackend(storeID, workDir, includePatterns, tags),
	}
}

// Sync clones (first run) or pulls the repo, then lists the working tree.
func (b *gitRepoBackend) Sync(ctx context.Context) ([]DocRef, error) {
	if err := b.refresh(ctx); err != nil {
		return nil, err
	}
	return b.fs.Sync(ctx)
}

// Fetch reads a document from the local clone.
func (b *gitRepoBackend) Fetch(ctx context.Context, ref DocRef) (string, error) {
	return b.fs.Fetch(ctx, ref)
}

// List filters the local clone's documents.
func (b *gitRepoBackend) List(ctx context.Context, filter DocFilter) ([]DocRef, error) {
	return b.fs.List(ctx, filter)
}

// refresh clones the repo if the working tree is absent, otherwise pulls.
func (b *gitRepoBackend) refresh(ctx context.Context) error {
	if b.runner == nil {
		return fmt.Errorf("docstore: git_repo %s: no git runner configured", b.storeID)
	}
	if isGitClone(b.workDir) {
		if err := b.runner.Pull(ctx, b.workDir, b.auth); err != nil {
			return fmt.Errorf("docstore: git_repo pull %s: %w", b.storeID, err)
		}
		return nil
	}
	if err := os.MkdirAll(b.workDir, 0o755); err != nil {
		return fmt.Errorf("docstore: git_repo mkdir %s: %w", b.workDir, err)
	}
	if err := b.runner.Clone(ctx, b.url, b.workDir, b.auth); err != nil {
		return fmt.Errorf("docstore: git_repo clone %s: %w", b.storeID, err)
	}
	return nil
}

// isGitClone reports whether dir already contains a .git directory.
func isGitClone(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

var _ Backend = (*gitRepoBackend)(nil)
