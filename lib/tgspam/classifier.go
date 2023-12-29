package tgspam

import "math"

// based on the code from https://github.com/RadhiFadlillah/go-bayesian/blob/master/classifier.go

// spamClass is alias of string, representing class of a document
type spamClass string

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
	sum := 0.0
	probs := make(map[spamClass]float64)

	// convert log probabilities to standard probabilities
	for _, logProb := range logProbs {
		sum += math.Exp(logProb)
	}

	// normalize probabilities
	for class, logProb := range logProbs {
		probs[class] = math.Exp(logProb) / sum
	}

	return probs
}
