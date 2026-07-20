package intent

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/types"
)

// DefaultThreshold is the minimum score for a match to be considered valid.
const DefaultThreshold = 0.2

// stopWords are common English words that add noise to similarity scoring.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "to": true, "for": true,
	"of": true, "in": true, "is": true, "it": true, "on": true,
	"and": true, "or": true, "with": true, "from": true, "by": true,
	"my": true, "me": true, "at": true,
}

// Matcher performs keyword-based intent matching against a set of tools.
type Matcher struct {
	tools     []types.ToolSummary
	threshold float64
}

// ToolMatch represents a match result with a score and reason.
type ToolMatch struct {
	Tool   types.ToolSummary
	Score  float64
	Reason string
}

// NewMatcher creates a new intent matcher with the given tools and default threshold.
func NewMatcher(tools []types.ToolSummary) *Matcher {
	return &Matcher{
		tools:     tools,
		threshold: DefaultThreshold,
	}
}

// Match finds the best matching tool for the given intent.
// Returns an error if no tool meets the threshold score.
func (m *Matcher) Match(intent string) (*ToolMatch, error) {
	matches := m.MatchAll(intent, 1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no matching tool found for intent %q (threshold=%.2f)",
			intent, m.threshold)
	}
	best := matches[0]
	if best.Score < m.threshold {
		return nil, fmt.Errorf("best match %q score %.2f below threshold %.2f",
			best.Tool.Name, best.Score, m.threshold)
	}
	return &best, nil
}

// MatchAll returns all tools matching the intent, sorted by score descending,
// up to the given limit. Tools below threshold are still included
// so callers can decide whether to accept low-confidence matches.
func (m *Matcher) MatchAll(intent string, limit int) []ToolMatch {
	intentTokens := tokenize(intent)
	intentTokens = removeStopWords(intentTokens)
	if len(intentTokens) == 0 {
		return nil
	}

	var matches []ToolMatch
	for _, tool := range m.tools {
		score, reason := scoreTool(intentTokens, tool)
		if score > 0 {
			matches = append(matches, ToolMatch{
				Tool:   tool,
				Score:  score,
				Reason: reason,
			})
		}
	}

	// Sort by score descending using insertion sort (small lists).
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].Score > matches[j-1].Score; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}

	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}

	return matches
}

// tokenize splits text into lowercase word tokens, removing punctuation.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// removeStopWords filters out common English stop words from a token list.
func removeStopWords(tokens []string) []string {
	var filtered []string
	for _, t := range tokens {
		if !stopWords[t] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return tokens // fallback: if all tokens are stop words, keep them
	}
	return filtered
}

// simpleStem applies basic English stemming rules.
// This covers common suffixes without a full Porter stemmer.
func simpleStem(word string) string {
	// Handle -ing suffix
	if strings.HasSuffix(word, "ing") && len(word) > 5 {
		stem := strings.TrimSuffix(word, "ing")
		// "writing" -> "writ" -> double consonant: "writt" -> "write"
		// "deleting" -> "delet" -> "delete"
		if len(stem) > 1 && stem[len(stem)-1] == stem[len(stem)-2] {
			stem = stem[:len(stem)-1]
		}
		if strings.HasSuffix(stem, "at") || strings.HasSuffix(stem, "it") {
			return stem + "e"
		}
		return stem
	}
	// Handle -tion/-sion
	if strings.HasSuffix(word, "tion") || strings.HasSuffix(word, "sion") {
		return word[:len(word)-4]
	}
	// Handle -ness
	if strings.HasSuffix(word, "ness") && len(word) > 5 {
		return word[:len(word)-4]
	}
	// Handle -ed past tense
	if strings.HasSuffix(word, "ed") && len(word) > 4 {
		return word[:len(word)-2]
	}
	// Handle -ly adverb
	if strings.HasSuffix(word, "ly") && len(word) > 4 {
		return word[:len(word)-2]
	}
	return word
}

// stemTokens applies simpleStem to each token in the list.
func stemTokens(tokens []string) []string {
	stemmed := make([]string, len(tokens))
	for i, t := range tokens {
		stemmed[i] = simpleStem(t)
	}
	return stemmed
}

// jaccardSimilarity computes the Jaccard similarity coefficient between two token sets.
// J(A,B) = |A ∩ B| / |A ∪ B|
func jaccardSimilarity(a, b []string) float64 {
	setA := make(map[string]bool, len(a))
	for _, t := range a {
		setA[t] = true
	}

	intersection := 0
	for _, t := range b {
		if setA[t] {
			intersection++
		}
	}

	union := len(setA) + len(b) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

// exactNameMatchBonus checks if any intent token appears in the tool name
// and returns a bonus score (0.05 per match) and count of matched tokens.
func exactNameMatchBonus(intentTokens []string, nameTokens []string) (float64, int) {
	nameSet := make(map[string]bool, len(nameTokens))
	for _, nt := range nameTokens {
		nameSet[nt] = true
	}

	matched := 0
	for _, it := range intentTokens {
		if nameSet[it] {
			matched++
		}
		// Also check if the raw intent token is a substring of any name token
		if matched == 0 {
			for _, nt := range nameTokens {
				if strings.Contains(nt, it) || strings.Contains(it, nt) {
					matched++
					break
				}
			}
		}
	}

	if matched > 0 {
		return float64(matched) * 0.05, matched
	}
	return 0, 0
}

// scoreTool computes a Jaccard similarity score for a tool against intent tokens.
//
// Scoring algorithm:
// 1. Tokenize both intent and tool (name + description), lowercase, remove stop words
// 2. Apply simple stemming to all tokens
// 3. Compute Jaccard similarity between token sets
// 4. Add 0.05 bonus per exact name match
// 5. Return the combined score
func scoreTool(intentTokens []string, tool types.ToolSummary) (float64, string) {
	nameTokens := tokenize(tool.Name)
	descTokens := tokenize(tool.Description)
	descTokens = removeStopWords(descTokens)

	// Combine name and description tokens for the tool side
	toolTokens := append(nameTokens, descTokens...)

	// Apply stemming for comparison
	intentStemmed := stemTokens(intentTokens)
	toolStemmed := stemTokens(toolTokens)

	// Compute Jaccard similarity on stemmed tokens
	score := jaccardSimilarity(intentStemmed, toolStemmed)

	if score == 0 {
		return 0, ""
	}

	// Apply exact name match bonus
	nameBonus, nameMatchCount := exactNameMatchBonus(intentTokens, nameTokens)
	score += nameBonus

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	// Build reason string
	var matchedWords []string
	intentSet := make(map[string]bool, len(intentStemmed))
	for _, t := range intentStemmed {
		intentSet[t] = true
	}
	for _, t := range toolStemmed {
		if intentSet[t] {
			matchedWords = append(matchedWords, t)
		}
	}

	reason := fmt.Sprintf("jaccard=%.3f name_bonus=%.2f (name_matches=%d) matched_stems=%v in %s",
		score-nameBonus, nameBonus, nameMatchCount, matchedWords, tool.Name)

	return score, reason
}
