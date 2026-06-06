package knowledge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchPath(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "internal/foo.go", true},
		{"*.md", "main.go", false},
		{"internal/**", "internal/a/b.go", true},
		{"internal/**/*.go", "internal/a/b.go", true},
		{"internal/**/*.go", "internal/a/b/c.go", true},
		{"internal/**", "cmd/main.go", false},
		{"**/node_modules/**", "a/node_modules/x/y.js", true},
		{"**/node_modules/**", "a/src/y.js", false},
		{"internal/svc/**", "internal/svc/a.go", true},
		{"internal/svc/**", "internal/types/a.go", false},
	}
	for _, tt := range tests {
		assert.Equalf(t, tt.want, MatchPath(tt.pattern, tt.path),
			"MatchPath(%q,%q)", tt.pattern, tt.path)
	}
}
