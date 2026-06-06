package docstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// S3Object is one object returned by an S3 List: its key and content.
type S3Object struct {
	// Key is the object key relative to the bucket/prefix.
	Key string
	// Content is the raw object body.
	Content []byte
}

// S3Client abstracts object storage for the s3 backend so tests inject a fake
// and CI makes no network call. List returns every object under prefix; Get
// returns one object's content. The concrete production client (AWS SDK or a
// signed-HTTP client) is decided at wiring time without affecting this contract.
type S3Client interface {
	List(ctx context.Context, prefix string) ([]S3Object, error)
	Get(ctx context.Context, key string) ([]byte, error)
}

// s3Backend is the s3 Doc Store backend (SPEC §10.2): it lists and fetches
// documents from object storage through an injected S3Client.
type s3Backend struct {
	storeID         string
	prefix          string
	includePatterns []string
	tags            []string
	client          S3Client
}

// newS3Backend constructs an s3 backend. prefix is the bucket-relative key
// prefix; client performs the object-store I/O (injected so tests use a fake).
func newS3Backend(storeID, prefix string, includePatterns, tags []string, client S3Client) *s3Backend {
	return &s3Backend{
		storeID:         storeID,
		prefix:          prefix,
		includePatterns: includePatterns,
		tags:            tags,
		client:          client,
	}
}

// Sync lists the prefix and returns a DocRef (with ContentHash) per object.
func (b *s3Backend) Sync(ctx context.Context) ([]DocRef, error) {
	if b.client == nil {
		return nil, fmt.Errorf("docstore: s3 %s: no client configured", b.storeID)
	}
	objs, err := b.client.List(ctx, b.prefix)
	if err != nil {
		return nil, fmt.Errorf("docstore: s3 list %s: %w", b.storeID, err)
	}
	refs := make([]DocRef, 0, len(objs))
	for _, o := range objs {
		key := strings.TrimPrefix(o.Key, b.prefix)
		key = strings.TrimPrefix(key, "/")
		if !b.included(key) {
			continue
		}
		refs = append(refs, DocRef{
			ID:          DocRefID(b.storeID, key),
			Title:       titleFor(key, o.Content),
			StoreID:     b.storeID,
			PathOrID:    key,
			ContentHash: HashContent(o.Content),
			Tags:        append([]string(nil), b.tags...),
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].PathOrID < refs[j].PathOrID })
	return refs, nil
}

// Fetch gets the object content for ref.
func (b *s3Backend) Fetch(ctx context.Context, ref DocRef) (string, error) {
	if b.client == nil {
		return "", fmt.Errorf("docstore: s3 %s: no client configured", b.storeID)
	}
	key := ref.PathOrID
	if b.prefix != "" {
		key = strings.TrimSuffix(b.prefix, "/") + "/" + key
	}
	content, err := b.client.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("docstore: s3 fetch %s: %w", ref.PathOrID, err)
	}
	return string(content), nil
}

// List re-syncs and applies the filter.
func (b *s3Backend) List(ctx context.Context, filter DocFilter) ([]DocRef, error) {
	refs, err := b.Sync(ctx)
	if err != nil {
		return nil, err
	}
	out := refs[:0]
	for _, r := range refs {
		if filter.matches(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// included reports whether the object key matches the include patterns.
func (b *s3Backend) included(key string) bool {
	if len(b.includePatterns) == 0 {
		return true
	}
	for _, pat := range b.includePatterns {
		if matchGlob(pat, key) {
			return true
		}
	}
	return false
}

var _ Backend = (*s3Backend)(nil)
