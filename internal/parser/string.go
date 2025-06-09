// Copyright © 2025 Prabhjot Singh Sethi, All Rights reserved
// Author: Prabhjot Singh Sethi <prabhjot.sethi@gmail.com>

package parser

import (
	"strings"
)

// map of irregular plural -> singular
var irregulars = map[string]string{
	"men":      "man",
	"women":    "woman",
	"children": "child",
	"feet":     "foot",
	"teeth":    "tooth",
	"geese":    "goose",
	"mice":     "mouse",
	"people":   "person",
}

func Plural2Singular(word string) (string, bool) {
	word = strings.ToLower(word)

	// Check for irregular nouns
	if singular, ok := irregulars[word]; ok {
		return singular, true
	}

	// Rule: -ies -> y
	if strings.HasSuffix(word, "ies") && len(word) > 3 {
		return word[:len(word)-3] + "y", true
	}

	// Rule: -es -> remove "es" for certain endings
	if strings.HasSuffix(word, "es") {
		if strings.HasSuffix(word, "ses") || strings.HasSuffix(word, "xes") ||
			strings.HasSuffix(word, "zes") || strings.HasSuffix(word, "ches") || strings.HasSuffix(word, "shes") {
			return word[:len(word)-2], true // Remove "es"
		}
	}

	// Rule: -s -> remove final "s"
	if strings.HasSuffix(word, "s") && len(word) > 3 &&
		!strings.HasSuffix(word, "ss") && !strings.HasSuffix(word, "us") {
		return word[:len(word)-1], true
	}

	// Assume it's already singular
	return word, false
}
