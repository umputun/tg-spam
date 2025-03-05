package tgspam

import (
	"fmt"
	"math"
)

// based on the code from https://github.com/RadhiFadlillah/go-bayesian/blob/master/classifier.go

// spamClass is alias of string, representing class of a document
type spamClass string

// enum for spamClass
const (
	ClassSpam spamClass = "spam"
	ClassHam  spamClass = "ham"
)

// document is a group of tokens with certain class
type document struct {
	spamClass spamClass
	tokens    []string
}

// newDocument return new document
func newDocument(class spamClass, tokens ...string) document {
	return document{
		spamClass: class,
		tokens:    tokens,
	}
}

// classifier is object for a classifying document
type classifier struct {
	learningResults    map[string]map[spamClass]int
	priorProbabilities map[spamClass]float64
	nDocumentByClass   map[spamClass]int
	nFrequencyByClass  map[spamClass]int
	nAllDocument       int
}

// newClassifier returns new classifier
func newClassifier() classifier {
	return classifier{
		learningResults:    make(map[string]map[spamClass]int),
		priorProbabilities: make(map[spamClass]float64),
		nDocumentByClass:   make(map[spamClass]int),
		nFrequencyByClass:  make(map[spamClass]int),
	}
}

// learn executes the learning process for this classifier
func (c *classifier) learn(docs ...document) {
	c.nAllDocument += len(docs)

	for _, doc := range docs {
		c.nDocumentByClass[doc.spamClass]++
		tokens := c.removeDuplicate(doc.tokens...)

		for _, token := range tokens {
			c.nFrequencyByClass[doc.spamClass]++

			if _, exist := c.learningResults[token]; !exist {
				c.learningResults[token] = make(map[spamClass]int)
			}

			c.learningResults[token][doc.spamClass]++
		}
	}

	for class, nDocument := range c.nDocumentByClass {
		c.priorProbabilities[class] = math.Log(float64(nDocument) / float64(c.nAllDocument))
	}
}

// unlearn removes the learning results for given documents
func (c *classifier) unlearn(docs ...document) error {
	if len(docs) > c.nAllDocument {
		return fmt.Errorf("trying to unlearn more documents than learned")
	}

	c.nAllDocument -= len(docs)

	for _, doc := range docs {
		if c.nDocumentByClass[doc.spamClass] <= 0 {
			return fmt.Errorf("no documents of class %v to unlearn", doc.spamClass)
		}

		c.nDocumentByClass[doc.spamClass]--
		tokens := c.removeDuplicate(doc.tokens...)

		for _, token := range tokens {
			if c.nFrequencyByClass[doc.spamClass] <= 0 {
				return fmt.Errorf("no tokens of class %v to unlearn", doc.spamClass)
			}
			c.nFrequencyByClass[doc.spamClass]--

			if c.learningResults[token][doc.spamClass] <= 0 {
				return fmt.Errorf("token %q not found in class %v", token, doc.spamClass)
			}
			c.learningResults[token][doc.spamClass]--

			// cleanup empty entries
			if c.learningResults[token][doc.spamClass] == 0 {
				delete(c.learningResults[token], doc.spamClass)
			}
			if len(c.learningResults[token]) == 0 {
				delete(c.learningResults, token)
			}
		}

		// cleanup empty class entries
		if c.nDocumentByClass[doc.spamClass] == 0 {
			delete(c.nDocumentByClass, doc.spamClass)
			delete(c.nFrequencyByClass, doc.spamClass)
			delete(c.priorProbabilities, doc.spamClass)
		} else {
			// update prior probability for the class
			c.priorProbabilities[doc.spamClass] = math.Log(float64(c.nDocumentByClass[doc.spamClass]) / float64(c.nAllDocument))
		}
	}

	return nil
}

// reset resets all learning results
func (c *classifier) reset() {
	c.learningResults = make(map[string]map[spamClass]int)
	c.priorProbabilities = make(map[spamClass]float64)
	c.nDocumentByClass = make(map[spamClass]int)
	c.nFrequencyByClass = make(map[spamClass]int)
	c.nAllDocument = 0
}

// classify executes the classifying process for tokens
func (c *classifier) classify(tokens ...string) (spamClass, float64, bool) {
	nVocabulary := len(c.learningResults)
	posteriorProbabilities := make(map[spamClass]float64)

	for class, priorProb := range c.priorProbabilities {
		posteriorProbabilities[class] = priorProb
	}
	tokens = c.removeDuplicate(tokens...)

	for class, freqByClass := range c.nFrequencyByClass {
		for _, token := range tokens {
			nToken := c.learningResults[token][class]
			posteriorProbabilities[class] += math.Log(float64(nToken+1) / float64(freqByClass+nVocabulary))
		}
	}

	probabilities := softmax(posteriorProbabilities) // apply softmax to posterior probabilities

	// find the best class and its probability
	var certain bool
	var bestClass spamClass
	var highestProb float64
	for class, prob := range probabilities {
		if highestProb == 0 || prob > highestProb {
			certain = true
			bestClass = class
			highestProb = prob
		} else if prob == highestProb {
			certain = false
		}
	}

	highestProb *= 100 // convert probability to percentage
	return bestClass, highestProb, certain
}

func (c *classifier) removeDuplicate(tokens ...string) []string {
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

// softmax converts log probabilities to normalized probabilities
func softmax(logProbs map[spamClass]float64) map[spamClass]float64 {
	if len(logProbs) == 0 {
		return nil
	}

	// step 1: Find the max value to subtract (prevents overflow)
	maxVal := math.Inf(-1) // start with negative infinity
	for _, v := range logProbs {
		if v > maxVal {
			maxVal = v
		}
	}

	// step 2: Compute exp(x - maxVal) and sum for normalization
	expSum := 0.0
	exps := make(map[spamClass]float64)
	for cat, v := range logProbs {
		exps[cat] = math.Exp(v - maxVal) // shift by maxVal keeps exp safe
		expSum += exps[cat]
	}

	// step 3: Normalize to get probabilities
	probs := make(map[spamClass]float64)
	for cat, v := range exps {
		probs[cat] = v / expSum // expSum > 0 since exp(x) > 0
	}
	return probs
}
