package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	good Class = "good"
	bad  Class = "bad"
)

func TestClassifier_Classify(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []string
		expected Class
		certain  bool
	}{
		{
			name:     "Tokens match good class",
			tokens:   []string{"tall", "handsome", "rich"},
			expected: good,
			certain:  true,
		},
		{
			name:     "Tokens partial match good class",
			tokens:   []string{"tall", "handsome", "happy"},
			expected: good,
			certain:  true,
		},
		{
			name:     "Tokens match bad class",
			tokens:   []string{"bald", "poor", "ugly"},
			expected: bad,
			certain:  true,
		},
		{
			name:     "Tokens match multiple classes",
			tokens:   []string{"tall", "poor"},
			expected: good,
			certain:  false,
		},
	}

	classifier := NewClassifier()
	classifier.Learn(
		NewDocument(good, "tall", "handsome", "rich"),
		NewDocument(bad, "bald", "poor", "ugly"),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, class, certain := classifier.Classify(tt.tokens...)
			assert.Equal(t, tt.expected, class)
			assert.Equal(t, tt.certain, certain)
		})
	}
}
