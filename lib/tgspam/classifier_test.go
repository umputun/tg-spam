package tgspam

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	good spamClass = "good"
	bad  spamClass = "bad"
)

func TestClassifier_Classify(t *testing.T) {
	tests := []struct {
		name        string
		tokens      []string
		expected    spamClass
		certain     bool
		probability float64
	}{
		{
			name:        "tokens match good class",
			tokens:      []string{"tall", "handsome", "rich"},
			expected:    good,
			certain:     true,
			probability: 88.88,
		},
		{
			name:        "tokens partial match good class",
			tokens:      []string{"tall", "handsome", "happy"},
			expected:    good,
			certain:     true,
			probability: 80.00,
		},
		{
			name:        "tokens match bad class",
			tokens:      []string{"bald", "poor", "ugly"},
			expected:    bad,
			certain:     true,
			probability: 88.88,
		},
		{
			name:        "tokens match multiple classes",
			tokens:      []string{"tall", "poor", "healthy", "wealthy", "happy", "handsome"},
			expected:    good,
			certain:     true,
			probability: 66.66,
		},
		{
			name:        "certain is false when tokens match the same",
			tokens:      []string{"average", "content", "handsome", "ugly"},
			certain:     false,
			probability: 50,
		},
	}

	classifier := newClassifier()
	classifier.learn(
		newDocument(good, "tall", "handsome", "rich"),
		newDocument(bad, "bald", "poor", "ugly"),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, p, certain := classifier.classify(tt.tokens...)
			t.Logf("probability: %v", p)
			assert.InDelta(t, tt.probability, p, 0.01, "probability")
			if !tt.certain {
				assert.Equal(t, tt.certain, certain, "certainty")
				return
			}
			assert.Equal(t, tt.expected, class, "class")
		})
	}
}
