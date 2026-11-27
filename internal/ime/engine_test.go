package ime

import (
	"reflect"
	"strings"
	"testing"
)

func typeKeys(engine *Engine, keys string) {
	for i := 0; i < len(keys); i++ {
		engine.Handle(keys[i : i+1])
	}
}

func TestCommonWords(t *testing.T) {
	tests := []struct {
		input  string
		text   string
		pinyin string
	}{
		{input: "nihao", text: "你好", pinyin: "ni hao"},
		{input: "zhongwen", text: "中文", pinyin: "zhong wen"},
		{input: "shurufa", text: "输入法", pinyin: "shu ru fa"},
		{input: "windows95", text: "windows95", pinyin: "windows95"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			engine := New()
			typeKeys(engine, test.input)
			if got := engine.Composition(); got != test.input {
				t.Fatalf("composition = %q, want %q", got, test.input)
			}
			candidates := engine.Candidates()
			if len(candidates) == 0 {
				t.Fatal("expected candidates")
			}
			first := candidates[0]
			if first.Text != test.text || first.Pinyin != test.pinyin {
				t.Fatalf("first candidate = %#v, want text %q pinyin %q", first, test.text, test.pinyin)
			}
			if first.Frequency <= 0 {
				t.Fatalf("candidate frequency = %d, want positive", first.Frequency)
			}

			result := engine.Handle("Space")
			if !result.Handled || result.Committed != test.text {
				t.Fatalf("Space result = %#v, want committed %q", result, test.text)
			}
			if engine.Composition() != "" || len(engine.Candidates()) != 0 {
				t.Fatal("composition was not cleared after commit")
			}
		})
	}
}

func TestPartialPinyinAndPrefixCandidates(t *testing.T) {
	engine := New()
	typeKeys(engine, "niha")
	if got := engine.Candidates(); len(got) == 0 || got[0].Text != "你好" {
		t.Fatalf("candidates for niha = %#v, want 你好 first", got)
	}

	typeKeys(engine, "o")
	if got := engine.Candidates(); len(got) == 0 || got[0].Text != "你好" {
		t.Fatalf("candidates for nihao = %#v, want 你好 first", got)
	}
}

func TestWindows95LiteralDigits(t *testing.T) {
	engine := New()
	typeKeys(engine, "windows")
	if len(engine.Candidates()) >= 9 {
		t.Fatal("test setup requires fewer than nine windows candidates")
	}
	if result := engine.Handle("9"); result.Committed != "" || engine.Composition() != "windows9" {
		t.Fatalf("digit 9 result = %#v, composition = %q", result, engine.Composition())
	}
	if result := engine.Handle("5"); result.Committed != "" || engine.Composition() != "windows95" {
		t.Fatalf("digit 5 result = %#v, composition = %q", result, engine.Composition())
	}
	if result := engine.Handle("Enter"); result.Committed != "windows95" {
		t.Fatalf("Enter committed %q, want windows95", result.Committed)
	}
}

func TestDigitSelectionAndClearAfterCommit(t *testing.T) {
	engine := New()
	typeKeys(engine, "zhong")
	candidates := engine.Candidates()
	if len(candidates) < 2 {
		t.Fatalf("got %d candidates, need at least two", len(candidates))
	}
	if candidates[0].Text != "中" || candidates[1].Text != "种" {
		t.Fatalf("unexpected candidate order: %#v", candidates[:2])
	}

	result := engine.Handle("2")
	if !result.Handled || result.Committed != "种" {
		t.Fatalf("digit selection = %#v, want 种", result)
	}
	if engine.Composition() != "" || len(engine.Candidates()) != 0 {
		t.Fatal("digit selection did not clear the composition")
	}

	typeKeys(engine, "guo")
	if result := engine.Handle("Enter"); result.Committed != "国" {
		t.Fatalf("next composition committed %q, want 国", result.Committed)
	}
}

func TestZeroSelectsTenthCandidate(t *testing.T) {
	engine := New()
	typeKeys(engine, "shi")
	candidates := engine.Candidates()
	if len(candidates) != MaxCandidates {
		t.Fatalf("got %d candidates, want %d", len(candidates), MaxCandidates)
	}
	want := candidates[MaxCandidates-1].Text
	if result := engine.Handle("0"); result.Committed != want {
		t.Fatalf("zero selected %q, want tenth candidate %q", result.Committed, want)
	}
}

func TestBackspaceResegmentsAndEdits(t *testing.T) {
	engine := New()
	typeKeys(engine, "nihao")
	if got, want := engine.Syllables(), []string{"ni", "hao"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("syllables = %#v, want %#v", got, want)
	}

	result := engine.Handle("Backspace")
	if !result.Handled || engine.Composition() != "niha" {
		t.Fatalf("Backspace result = %#v, composition = %q", result, engine.Composition())
	}
	if got, want := engine.Syllables(), []string{"ni", "ha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("syllables after Backspace = %#v, want %#v", got, want)
	}
	if candidates := result.Candidates; len(candidates) == 0 || candidates[0].Text != "你好" {
		t.Fatalf("candidates after Backspace = %#v, want 你好", candidates)
	}

	typeKeys(engine, "o")
	if result := engine.Handle("Backspace"); result.Committed != "" || engine.Composition() != "niha" {
		t.Fatalf("second Backspace = %#v, composition = %q", result, engine.Composition())
	}
	typeKeys(engine, "o")
	if result := engine.Handle("Space"); result.Committed != "你好" {
		t.Fatalf("edited composition committed %q, want 你好", result.Committed)
	}

	typeKeys(engine, "shurufa")
	engine.Handle("Backspace")
	if engine.Composition() != "shuruf" {
		t.Fatalf("composition = %q, want shuruf", engine.Composition())
	}
	typeKeys(engine, "a")
	if result := engine.Handle("Enter"); result.Committed != "输入法" {
		t.Fatalf("edited shurufa committed %q, want 输入法", result.Committed)
	}
}

func TestBackspaceToEmpty(t *testing.T) {
	engine := New()
	typeKeys(engine, "nihao")
	for i := 0; i < len("nihao"); i++ {
		engine.Handle("Backspace")
	}
	if engine.Composition() != "" || len(engine.Candidates()) != 0 || len(engine.Syllables()) != 0 {
		t.Fatal("backspace did not return the engine to an empty state")
	}
	if result := engine.Handle("Backspace"); !result.Handled || result.Committed != "" {
		t.Fatalf("Backspace on empty engine = %#v", result)
	}
}

func TestCancelAndClear(t *testing.T) {
	engine := New()
	typeKeys(engine, "zhongwen")
	result := engine.Handle("Esc")
	if !result.Handled || result.Committed != "" {
		t.Fatalf("Esc result = %#v", result)
	}
	if engine.Composition() != "" || len(engine.Candidates()) != 0 {
		t.Fatal("Esc did not cancel the composition")
	}

	typeKeys(engine, "shurufa")
	engine.Clear()
	if engine.Composition() != "" || len(engine.Candidates()) != 0 {
		t.Fatal("Clear did not cancel the composition")
	}
}

func TestEmptyAndFallbackCommits(t *testing.T) {
	engine := New()
	if result := engine.Handle("Space"); result.Committed != " " {
		t.Fatalf("empty Space committed %q, want a space", result.Committed)
	}
	if result := engine.Handle("Enter"); result.Committed != "\n" {
		t.Fatalf("empty Enter committed %q, want a newline", result.Committed)
	}
	if result := engine.Handle("7"); result.Committed != "7" || engine.Composition() != "" {
		t.Fatalf("empty digit result = %#v, composition = %q", result, engine.Composition())
	}

	typeKeys(engine, "xyz")
	if len(engine.Candidates()) != 0 {
		t.Fatal("unexpected dictionary match for xyz")
	}
	if result := engine.Handle("Space"); result.Committed != "xyz" {
		t.Fatalf("fallback committed %q, want xyz", result.Committed)
	}
	if engine.Composition() != "" {
		t.Fatal("fallback commit did not clear the composition")
	}
}

func TestUnknownKeysAreIgnored(t *testing.T) {
	engine := New()
	for _, key := range []string{"", "Tab", "A", "ni hao", "Shift"} {
		if result := engine.Handle(key); result.Handled || result.Committed != "" || len(result.Candidates) != 0 {
			t.Fatalf("Handle(%q) = %#v, want ignored", key, result)
		}
	}
	if engine.Composition() != "" {
		t.Fatalf("unknown keys changed composition to %q", engine.Composition())
	}
}

func TestCandidatesAreFreshCopies(t *testing.T) {
	engine := New()
	typeKeys(engine, "zhong")
	first := engine.Candidates()
	if len(first) == 0 {
		t.Fatal("expected candidates")
	}
	original := first[0]
	first[0].Text = "changed"
	second := engine.Candidates()
	if second[0] != original {
		t.Fatalf("candidate slice aliases engine state: got %#v, want %#v", second[0], original)
	}
}

func TestSyllableSegmentation(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{input: "nihao", want: []string{"ni", "hao"}},
		{input: "zhongwen", want: []string{"zhong", "wen"}},
		{input: "shurufa", want: []string{"shu", "ru", "fa"}},
		{input: "nihaoh", want: []string{"ni", "hao", "h"}},
		{input: "windows95", want: []string{"windows95"}},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			engine := New()
			typeKeys(engine, test.input)
			if got := engine.Syllables(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Syllables() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestBundledDictionary(t *testing.T) {
	if len(builtinDictionary) < 300 {
		t.Fatalf("bundled dictionary has %d entries, want at least 300", len(builtinDictionary))
	}
	required := map[string]bool{
		"你好\x00ni hao":           false,
		"中文\x00zhong wen":        false,
		"输入法\x00shu ru fa":       false,
		"windows95\x00windows95": false,
	}
	for _, entry := range builtinDictionary {
		if entry.text == "" || entry.pinyin == "" || entry.frequency <= 0 {
			t.Fatalf("invalid dictionary entry: %#v", entry)
		}
		if entry.letters != strings.Join(entry.syllables, "") {
			t.Fatalf("inconsistent dictionary entry: %#v", entry)
		}
		if _, ok := required[entry.text+"\x00"+entry.pinyin]; ok {
			required[entry.text+"\x00"+entry.pinyin] = true
		}
	}
	for entry, found := range required {
		if !found {
			t.Errorf("dictionary is missing %q", strings.ReplaceAll(entry, "\x00", " / "))
		}
	}
}
