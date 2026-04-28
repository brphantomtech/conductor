package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpand(t *testing.T) {
	t.Parallel()

	env := func(set map[string]string) LookupFunc {
		return func(name string) (string, bool) {
			v, ok := set[name]
			return v, ok
		}
	}

	tests := []struct {
		name    string
		in      string
		env     map[string]string
		want    string
		wantErr error
	}{
		{
			name: "no_dollar_pass_through",
			in:   "no/expansion/here",
			env:  map[string]string{},
			want: "no/expansion/here",
		},
		{
			name: "bare_var",
			in:   "$FOO/bar",
			env:  map[string]string{"FOO": "/home/edson"},
			want: "/home/edson/bar",
		},
		{
			name: "braced_var",
			in:   "${FOO}_${BAR}",
			env:  map[string]string{"FOO": "x", "BAR": "y"},
			want: "x_y",
		},
		{
			name: "double_dollar_literal",
			in:   "cost is $$5.00",
			env:  map[string]string{},
			want: "cost is $5.00",
		},
		{
			name: "lone_dollar_preserved",
			in:   "$ alone",
			env:  map[string]string{},
			want: "$ alone",
		},
		{
			name: "set_but_empty_is_ok",
			in:   "value=$EMPTY",
			env:  map[string]string{"EMPTY": ""},
			want: "value=",
		},
		{
			name:    "unset_var_errors",
			in:      "value=$NOPE",
			env:     map[string]string{},
			wantErr: ErrUnsetVariable,
		},
		{
			name:    "unset_braced_var_errors",
			in:      "value=${NOPE}",
			env:     map[string]string{},
			wantErr: ErrUnsetVariable,
		},
		{
			name: "trailing_dollar",
			in:   "value=$",
			env:  map[string]string{},
			want: "value=$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Expand(tt.in, env(tt.env))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExpand_NilLookupUsesOSEnv(t *testing.T) {
	t.Setenv("CONDUCTOR_TEST_VAR", "from_os")
	got, err := Expand("$CONDUCTOR_TEST_VAR", nil)
	require.NoError(t, err)
	require.Equal(t, "from_os", got)
}

func TestExpand_DistinguishesUnsetFromEmpty(t *testing.T) {
	t.Parallel()
	// SPEC §6.4 must be able to fail on missing tracker keys: an unset
	// variable is a hard error, while a set-but-empty variable expands to
	// the empty string and bubbles up as a missing-required-field rather
	// than a missing-env-var.
	emptyEnv := func(name string) (string, bool) { return "", true }

	got, err := Expand("$EMPTY", emptyEnv)
	require.NoError(t, err)
	require.Equal(t, "", got)

	unsetEnv := func(name string) (string, bool) { return "", false }
	_, err = Expand("$UNSET", unsetEnv)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsetVariable))
}
