package tgspam

import (
	"fmt"
	"strings"
	"unicode"
)

// scriptAliases maps friendly language names to unicode.Scripts table names. an
// alias may expand to more than one script (japanese uses two syllabaries).
var scriptAliases = map[string][]string{
	"chinese":   {"Han"},
	"russian":   {"Cyrillic"},
	"ukrainian": {"Cyrillic"},
	"arabic":    {"Arabic"},
	"korean":    {"Hangul"},
	"japanese":  {"Hiragana", "Katakana"},
	"hebrew":    {"Hebrew"},
	"thai":      {"Thai"},
	"greek":     {"Greek"},
}

// ResolveProhibitedScripts turns a list of language/script names into a map of
// unicode.Scripts name to its range table. friendly aliases (chinese, russian,
// japanese, ...) are expanded via scriptAliases; other entries are matched
// case-insensitively against unicode.Scripts keys. empty and whitespace-only
// entries are skipped; an unknown name returns an error naming it. an empty
// input yields an empty map and nil error (feature disabled).
func ResolveProhibitedScripts(names []string) (map[string]*unicode.RangeTable, error) {
	result := make(map[string]*unicode.RangeTable)
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if scripts, ok := scriptAliases[name]; ok {
			for _, s := range scripts {
				result[s] = unicode.Scripts[s]
			}
			continue
		}
		matched := false
		for scriptName, table := range unicode.Scripts {
			if strings.EqualFold(scriptName, name) {
				result[scriptName] = table
				matched = true
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("unknown prohibited script or language: %q", strings.TrimSpace(raw))
		}
	}
	return result, nil
}

// ValidateProhibitedLangs resolves the comma-separated prohibited language/script
// list and enforces that minCount is >= 1 whenever the list resolves to at least
// one script. an empty, blank, or delimiter-only list resolves to no scripts and
// disables the feature, so it imposes no minimum. the gate is the resolver's
// result, not the raw string, so " , " is correctly treated as disabled. returns
// nil when the configuration is valid. shared by startup validation, the
// save-config command, and the web settings save path.
func ValidateProhibitedLangs(langs string, minCount int) error {
	resolved, err := ResolveProhibitedScripts(strings.Split(langs, ","))
	if err != nil {
		return fmt.Errorf("prohibited-langs: %w", err)
	}
	if len(resolved) > 0 && minCount < 1 {
		return fmt.Errorf("prohibited-langs-min (%d) must be >= 1 when prohibited-langs is set", minCount)
	}
	return nil
}
