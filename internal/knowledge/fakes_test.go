package knowledge

import (
	"context"
	"errors"
	"sync"
	"time"
)

// fakeSummarizer returns a fixed summary and counts calls so cache behavior can
// be asserted.
type fakeSummarizer struct {
	mu     sync.Mutex
	calls  int
	prefix string
}

func (f *fakeSummarizer) Summarize(_ context.Context, path, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.prefix + path, nil
}

func (f *fakeSummarizer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeEmbedder returns a deterministic vector derived from the text and counts
// calls. Optionally fails to exercise error classification.
type fakeEmbedder struct {
	mu    sync.Mutex
	calls int
	fail  bool
	dim   int
}

func newFakeEmbedder() *fakeEmbedder { return &fakeEmbedder{dim: 64} }

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return nil, errors.New("embed boom")
	}
	h := newHashEmbedder(f.dim)
	return h.Embed(context.Background(), text)
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fixedClock returns a constant time for deterministic LastIndexedAt values.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }
