package fts5

import (
	"strings"
	"unicode"
)

// Common English stop words to filter out from queries.
var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true,
	"at": true, "be": true, "by": true, "for": true, "from": true,
	"has": true, "he": true, "in": true, "is": true, "it": true,
	"its": true, "of": true, "on": true, "or": true, "that": true,
	"the": true, "to": true, "was": true, "were": true, "will": true,
	"with": true, "this": true, "but": true, "they": true, "have": true,
	"had": true, "not": true, "what": true, "all": true, "can": true,
	"her": true, "which": true, "do": true, "if": true, "we": true,
	"my": true, "so": true, "no": true, "i": true, "me": true,
	"you": true, "your": true,
}

// buildFTSQuery converts a natural language query into an FTS5 MATCH expression.
// Uses OR semantics — any term matches.
func buildFTSQuery(query string) string {
	terms := extractTerms(query)
	if len(terms) == 0 {
		return ""
	}
	// FTS5 OR query: term1 OR term2 OR term3
	return strings.Join(terms, " OR ")
}

// extractTerms splits a query into searchable terms, filtering stop words.
func extractTerms(query string) []string {
	words := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var terms []string
	for _, w := range words {
		w = strings.ToLower(w)
		if len(w) < 2 {
			continue
		}
		if stopWords[w] {
			continue
		}
		// Escape FTS5 special characters by wrapping in double quotes.
		terms = append(terms, `"`+w+`"`)
	}
	return terms
}
