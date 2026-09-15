package tgspam

import (
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProhibitedScripts(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string // expected unicode.Scripts keys in the result
	}{
		{name: "empty input", input: nil, want: nil},
		{name: "empty slice", input: []string{}, want: nil},
		{name: "whitespace-only entry", input: []string{"  ", "\t"}, want: nil},
		{name: "chinese alias", input: []string{"chinese"}, want: []string{"Han"}},
		{name: "japanese alias expands to two", input: []string{"japanese"}, want: []string{"Hiragana", "Katakana"}},
		{name: "russian alias", input: []string{"russian"}, want: []string{"Cyrillic"}},
		{name: "korean alias", input: []string{"korean"}, want: []string{"Hangul"}},
		{name: "alias case-insensitive", input: []string{"Chinese"}, want: []string{"Han"}},
		{name: "alias with surrounding spaces", input: []string{"  chinese  "}, want: []string{"Han"}},
		{name: "raw script name Cyrillic", input: []string{"Cyrillic"}, want: []string{"Cyrillic"}},
		{name: "raw script name Arabic", input: []string{"Arabic"}, want: []string{"Arabic"}},
		{name: "raw script case-insensitive", input: []string{"arabic"}, want: []string{"Arabic"}},
		{name: "multiple entries", input: []string{"chinese", "Cyrillic"}, want: []string{"Han", "Cyrillic"}},
		{name: "dedup same script", input: []string{"russian", "ukrainian"}, want: []string{"Cyrillic"}},
		{name: "skips empty among valid", input: []string{"chinese", "", "arabic"}, want: []string{"Han", "Arabic"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveProhibitedScripts(tt.input)
			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for _, key := range tt.want {
				table, ok := got[key]
				assert.True(t, ok, "expected script %q in result", key)
				assert.Same(t, unicode.Scripts[key], table, "table for %q must be the unicode.Scripts entry", key)
			}
		})
	}
}

func TestResolveProhibitedScripts_UnknownName(t *testing.T) {
	tests := []struct {
		name  string
		input []string
	}{
		{name: "unknown language", input: []string{"klingon"}},
		{name: "unknown among valid", input: []string{"chinese", "klingon"}},
		{name: "typo in script name", input: []string{"Cyrilic"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveProhibitedScripts(tt.input)
			require.Error(t, err)
			assert.Nil(t, got)
		})
	}
}

func TestResolveProhibitedScripts_ErrorNamesOffender(t *testing.T) {
	_, err := ResolveProhibitedScripts([]string{"chinese", "  klingon  "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "klingon")
}

func TestValidateProhibitedLangs(t *testing.T) {
	tests := []struct {
		name     string
		langs    string
		minCount int
		wantErr  string
	}{
		{name: "empty disabled", langs: "", minCount: 0, wantErr: ""},
		{name: "whitespace-only disabled", langs: "  ", minCount: 0, wantErr: ""},
		{name: "delimiter-only disabled", langs: " , ", minCount: 0, wantErr: ""},
		{name: "alias with valid min", langs: "chinese", minCount: 3, wantErr: ""},
		{name: "multiple with valid min", langs: "chinese, cyrillic, Arabic", minCount: 2, wantErr: ""},
		{name: "unknown script rejected", langs: "klingon", minCount: 3, wantErr: `prohibited-langs: unknown prohibited script or language: "klingon"`},
		{name: "non-empty with min zero rejected", langs: "chinese", minCount: 0, wantErr: "prohibited-langs-min (0) must be >= 1 when prohibited-langs is set"},
		{name: "non-empty with min negative rejected", langs: "chinese", minCount: -1, wantErr: "prohibited-langs-min (-1) must be >= 1 when prohibited-langs is set"},
		{name: "disabled ignores min zero", langs: " , ", minCount: 0, wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProhibitedLangs(tt.langs, tt.minCount)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}
