package bot

// based on the code from https://github.com/RadhiFadlillah/go-bayesian/blob/master/classifier.go

import (
	"math"
)

// Class is alias of string, representing class of a document
type Class string

// Document is a group of tokens with certain class
type Document struct {
	Class  Class
	Tokens []string
}

// NewDocument return new Document
func NewDocument(class Class, tokens ...string) Document {
	return Document{
		Class:  class,
		Tokens: tokens,
	}
}

// Classifier is object for a classifying document
type Classifier struct {
	LearningResults    map[string]map[Class]int
	PriorProbabilities map[Class]float64
	NDocumentByClass   map[Class]int
	NFrequencyByClass  map[Class]int
	NAllDocument       int
}

// NewClassifier returns new Classifier
func NewClassifier() Classifier {
	return Classifier{
		LearningResults:    make(map[string]map[Class]int),
		PriorProbabilities: make(map[Class]float64),
		NDocumentByClass:   make(map[Class]int),
		NFrequencyByClass:  make(map[Class]int),
	}
}

// Learn executes the learning process for this classifier
func (c *Classifier) Learn(docs ...Document) {
	c.NAllDocument += len(docs)

	for _, doc := range docs {
		c.NDocumentByClass[doc.Class]++
		tokens := c.removeDuplicate(doc.Tokens...)

		for _, token := range tokens {
			c.NFrequencyByClass[doc.Class]++

			if _, exist := c.LearningResults[token]; !exist {
				c.LearningResults[token] = make(map[Class]int)
			}

			c.LearningResults[token][doc.Class]++
		}
	}

	for class, nDocument := range c.NDocumentByClass {
		c.PriorProbabilities[class] = math.Log(float64(nDocument) / float64(c.NAllDocument))
	}
}

// Reset resets all learning results
func (c *Classifier) Reset() {
	c.LearningResults = make(map[string]map[Class]int)
	c.PriorProbabilities = make(map[Class]float64)
	c.NDocumentByClass = make(map[Class]int)
	c.NFrequencyByClass = make(map[Class]int)
	c.NAllDocument = 0
}

// Classify executes the classifying process for tokens
func (c *Classifier) Classify(tokens ...string) (map[Class]float64, Class, bool) {
	nVocabulary := len(c.LearningResults)
	posteriorProbabilities := make(map[Class]float64)

	for class, priorProb := range c.PriorProbabilities {
		posteriorProbabilities[class] = priorProb
	}
	tokens = c.removeDuplicate(tokens...)

	for class, freqByClass := range c.NFrequencyByClass {
		for _, token := range tokens {
			nToken := c.LearningResults[token][class]
			posteriorProbabilities[class] += math.Log(float64(nToken+1) / float64(freqByClass+nVocabulary))
		}
	}

	var certain bool
	var bestClass Class
	var highestProb float64
	for class, prob := range posteriorProbabilities {
		if highestProb == 0 || prob > highestProb {
			certain = true
			bestClass = class
			highestProb = prob
		} else if prob == highestProb {
			certain = false
		}
	}

	return posteriorProbabilities, bestClass, certain
}

func (c *Classifier) removeDuplicate(tokens ...string) []string {
	mapTokens := make(map[string]struct{})
	newTokens := []string{}

	for _, token := range tokens {
		mapTokens[token] = struct{}{}
	}

	for key := range mapTokens {
		newTokens = append(newTokens, key)
	}

	return newTokens
}
