// Package ime implements a small, dependency-free Chinese pinyin input engine.
package ime

import (
	"sort"
	"strings"
)

// MaxCandidates is the maximum number of candidates returned for a composition.
const MaxCandidates = 10

// Candidate is one dictionary match for the current pinyin composition.
// Pinyin uses lower-case, tone-free keyboard spellings, with syllables separated
// by spaces. Frequency is a relative rank; larger values are more common.
type Candidate struct {
	Text      string
	Pinyin    string
	Frequency int
}

// Result describes the effect of one normalized keyboard event. Candidates are
// ordered best-first. Committed is text that should be inserted into the caller's
// document. Handled is false for events outside the engine's normalized input.
type Result struct {
	Candidates []Candidate
	Committed  string
	Handled    bool
}

// Engine keeps the current pinyin composition. It is intended for use from a
// single UI event loop.
type Engine struct {
	input      string
	dictionary []dictionaryEntry
	syllables  map[string]struct{}
}

// New returns an engine initialized with the bundled compact dictionary.
func New() *Engine {
	syllables := make(map[string]struct{})
	for _, entry := range builtinDictionary {
		for _, syllable := range entry.syllables {
			if isKeyboardLetters(syllable) {
				syllables[syllable] = struct{}{}
			}
		}
	}
	return &Engine{dictionary: builtinDictionary, syllables: syllables}
}

// Handle consumes a normalized key: a-z, 0-9, Backspace, Space, Enter, or Esc.
// Letters and literal digits extend the composition. Digits 1-9 select the
// corresponding candidate and 0 selects the tenth; if no such candidate exists,
// the digit is treated as literal input so mixed text such as windows95 works.
// Space and Enter commit the first candidate (or the raw composition when there
// is no match). With an empty composition they insert a space or newline.
// Esc cancels the composition. Every commit or cancellation clears the engine.
func (e *Engine) Handle(key string) Result {
	switch key {
	case "Backspace":
		if len(e.input) > 0 {
			e.input = e.input[:len(e.input)-1]
		}
		return Result{Candidates: e.Candidates(), Handled: true}
	case "Space":
		return e.commit(" ")
	case "Enter":
		return e.commit("\n")
	case "Esc":
		e.Clear()
		return Result{Handled: true}
	}

	if len(key) != 1 {
		return Result{}
	}

	ch := key[0]
	if ch >= 'a' && ch <= 'z' {
		e.input += key
		return Result{Candidates: e.Candidates(), Handled: true}
	}
	if ch >= '0' && ch <= '9' {
		if e.input == "" {
			return Result{Committed: key, Handled: true}
		}

		candidates := e.Candidates()
		index := int(ch - '0')
		if index == 0 {
			index = MaxCandidates
		}
		if index <= len(candidates) {
			committed := candidates[index-1].Text
			e.Clear()
			return Result{Committed: committed, Handled: true}
		}

		e.input += key
		return Result{Candidates: e.Candidates(), Handled: true}
	}

	return Result{}
}

// Composition returns the raw keyboard spelling currently being edited.
func (e *Engine) Composition() string {
	return e.input
}

// Candidates returns a fresh, best-first slice of matches for the composition.
func (e *Engine) Candidates() []Candidate {
	if e.input == "" {
		return nil
	}

	type rankedCandidate struct {
		candidate Candidate
		exact     bool
	}

	matches := make([]rankedCandidate, 0, MaxCandidates)
	seen := make(map[Candidate]struct{})
	for _, entry := range e.dictionary {
		if !strings.HasPrefix(entry.letters, e.input) {
			continue
		}
		candidate := Candidate{
			Text:      entry.text,
			Pinyin:    entry.pinyin,
			Frequency: entry.frequency,
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		matches = append(matches, rankedCandidate{
			candidate: candidate,
			exact:     entry.letters == e.input,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		left, right := matches[i], matches[j]
		if left.exact != right.exact {
			return left.exact
		}
		if left.candidate.Frequency != right.candidate.Frequency {
			return left.candidate.Frequency > right.candidate.Frequency
		}
		if left.candidate.Text != right.candidate.Text {
			return left.candidate.Text < right.candidate.Text
		}
		return left.candidate.Pinyin < right.candidate.Pinyin
	})

	count := len(matches)
	if count > MaxCandidates {
		count = MaxCandidates
	}
	candidates := make([]Candidate, count)
	for i := 0; i < count; i++ {
		candidates[i] = matches[i].candidate
	}
	return candidates
}

// Syllables returns a best-effort segmentation of the composition. Completed
// syllables use the longest valid dictionary spelling; an incomplete or
// non-pinyin tail is retained as the final segment. Ambiguous dictionary matches
// are still all considered by Candidates.
func (e *Engine) Syllables() []string {
	if e.input == "" {
		return nil
	}

	const maxSyllableLength = 6
	segments := make([]string, 0, 4)
	for rest := e.input; rest != ""; {
		end := 0
		limit := len(rest)
		if limit > maxSyllableLength {
			limit = maxSyllableLength
		}
		for i := 1; i <= limit; i++ {
			if _, ok := e.syllables[rest[:i]]; ok {
				end = i
			}
		}
		if end == 0 {
			segments = append(segments, rest)
			break
		}
		segments = append(segments, rest[:end])
		rest = rest[end:]
	}
	return segments
}

// Clear cancels the current composition without committing text.
func (e *Engine) Clear() {
	e.input = ""
}

func (e *Engine) commit(emptyFallback string) Result {
	if e.input == "" {
		return Result{Committed: emptyFallback, Handled: true}
	}

	committed := e.input
	if candidates := e.Candidates(); len(candidates) > 0 {
		committed = candidates[0].Text
	}
	e.Clear()
	return Result{Committed: committed, Handled: true}
}

func isKeyboardLetters(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 'a' || value[i] > 'z' {
			return false
		}
	}
	return true
}
