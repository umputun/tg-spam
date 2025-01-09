package tgspam

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestClassifier_Unlearn(t *testing.T) {
	assert := require.New(t)

	t.Run("basic unlearn", func(t *testing.T) {
		c := newClassifier()
		doc := newDocument("spam", "bad", "words")

		c.learn(doc)
		assert.Equal(1, c.nAllDocument)
		assert.Equal(1, c.nDocumentByClass["spam"])

		err := c.unlearn(doc)
		assert.NoError(err)
		assert.Equal(0, c.nAllDocument)
		assert.Empty(c.nDocumentByClass)
		assert.Empty(c.learningResults)
	})

	t.Run("unlearn with multiple docs", func(t *testing.T) {
		c := newClassifier()
		docs := []document{
			newDocument("spam", "bad", "words"),
			newDocument("ham", "good", "words"),
		}

		c.learn(docs...)
		err := c.unlearn(docs[0])
		assert.NoError(err)
		assert.Equal(1, c.nAllDocument)
		assert.Equal(1, c.nDocumentByClass["ham"])
		assert.Empty(c.nDocumentByClass["spam"])
	})

	t.Run("errors", func(t *testing.T) {
		c := newClassifier()
		doc := newDocument("spam", "bad", "words")

		err := c.unlearn(doc)
		assert.Error(err)

		c.learn(doc)
		err = c.unlearn(doc, doc) // try to unlearn same doc twice
		assert.Error(err)
	})
}

func TestClassifier_LearnUnlearnIntegration(t *testing.T) {
	assert := require.New(t)

	t.Run("learn unlearn learn sequence", func(t *testing.T) {
		c := newClassifier()
		doc1 := newDocument(good, "nice", "friendly")
		doc2 := newDocument(bad, "mean", "unfriendly")

		// initial learning
		c.learn(doc1, doc2)
		class, prob, certain := c.classify("nice", "friendly")
		assert.Equal(good, class)
		assert.True(certain)
		assert.InDelta(80., prob, 0.01)

		// unlearn good class
		err := c.unlearn(doc1)
		assert.NoError(err)

		// verify only bad class remains
		class, prob, certain = c.classify("nice", "friendly")
		assert.Equal(bad, class)
		assert.True(certain)
		assert.InDelta(100., prob, 0.01)

		// learn good class again
		c.learn(doc1)
		class, prob, certain = c.classify("nice", "friendly")
		assert.Equal(good, class)
		assert.True(certain)
		assert.InDelta(80., prob, 0.01)
	})

	t.Run("unlearn with duplicate tokens", func(t *testing.T) {
		c := newClassifier()
		doc := newDocument(good, "nice", "nice", "nice") // duplicate tokens

		c.learn(doc)
		err := c.unlearn(doc)
		assert.NoError(err)
		assert.Empty(c.learningResults)
	})

	t.Run("learn unlearn with empty tokens", func(t *testing.T) {
		c := newClassifier()
		doc := newDocument(good)

		c.learn(doc)
		assert.Equal(1, c.nAllDocument)
		assert.Equal(1, c.nDocumentByClass[good])
		assert.Empty(c.learningResults) // no tokens to learn

		err := c.unlearn(doc)
		assert.NoError(err)
		assert.Empty(c.nDocumentByClass)
	})
}

func TestClassifier_ProbabilityConsistency(t *testing.T) {
	t.Run("probability is between 0 and 100", func(t *testing.T) {
		c := newClassifier()
		c.learn(
			newDocument(good, "a", "b"),
			newDocument(bad, "c", "d"),
		)

		// check good class tokens
		_, prob, _ := c.classify("a", "b")
		assert.Greater(t, prob, 50.0, "good class should have higher probability for its tokens")
		assert.LessOrEqual(t, prob, 100.0, "probability should not exceed 100")

		// check bad class tokens
		_, prob, _ = c.classify("c", "d")
		assert.Greater(t, prob, 50.0, "bad class should have higher probability for its tokens")
		assert.LessOrEqual(t, prob, 100.0, "probability should not exceed 100")

		// check mixed tokens
		_, prob, _ = c.classify("a", "d")
		assert.Greater(t, prob, 0.0, "probability should be positive")
		assert.LessOrEqual(t, prob, 100.0, "probability should not exceed 100")
	})
}

func TestClassifier_Reset(t *testing.T) {
	t.Run("learn unlearn reset sequence", func(t *testing.T) {
		c := newClassifier()
		doc := newDocument(good, "test")

		c.learn(doc)
		assert.NotEmpty(t, c.learningResults)

		c.reset()
		assert.Empty(t, c.learningResults)
		assert.Empty(t, c.priorProbabilities)
		assert.Empty(t, c.nDocumentByClass)
		assert.Empty(t, c.nFrequencyByClass)
		assert.Zero(t, c.nAllDocument)

		// should be able to learn again after reset
		c.learn(doc)
		assert.NotEmpty(t, c.learningResults)
	})
}
