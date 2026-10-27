package desktop

import "testing"

func TestNotepadSelectedTextAcrossLines(t *testing.T) {
	a := &NotepadApp{lines: []string{"hello world", "second line", "third"}}
	a.selAnchorLn, a.selAnchorCol = 0, 6
	a.caretLn, a.caretCol = 1, 6
	a.selActive = true

	if got, want := a.selectedText(), "world\nsecond"; got != want {
		t.Fatalf("selectedText = %q, want %q", got, want)
	}
}

func TestNotepadSelectionNormalizesReversedDrag(t *testing.T) {
	a := &NotepadApp{lines: []string{"alpha", "beta"}}
	a.selAnchorLn, a.selAnchorCol = 1, 4
	a.caretLn, a.caretCol = 0, 2
	a.selActive = true

	if got, want := a.selectedText(), "pha\nbeta"; got != want {
		t.Fatalf("selectedText = %q, want %q", got, want)
	}
}

func TestNotepadDeleteSelectionJoinsLines(t *testing.T) {
	a := &NotepadApp{lines: []string{"hello world", "second line", "third"}}
	a.selAnchorLn, a.selAnchorCol = 0, 6
	a.caretLn, a.caretCol = 1, 6
	a.selActive = true

	if !a.deleteSelection() {
		t.Fatal("deleteSelection reported no selection")
	}
	if len(a.lines) != 2 {
		t.Fatalf("lines = %q", a.lines)
	}
	if a.lines[0] != "hello  line" || a.lines[1] != "third" {
		t.Fatalf("lines = %q", a.lines)
	}
	if a.caretLn != 0 || a.caretCol != 6 {
		t.Fatalf("caret = %d:%d", a.caretLn, a.caretCol)
	}
	if a.selActive {
		t.Fatal("selection still active after deletion")
	}
}

func TestNotepadSelIntervalHandlesWrappedRows(t *testing.T) {
	a := &NotepadApp{lines: []string{"0123456789", "abcdefghij"}}
	a.selAnchorLn, a.selAnchorCol = 0, 4
	a.caretLn, a.caretCol = 1, 3
	a.selActive = true

	v := visLine{line: 0, start: 0, end: 10}
	s, e, ok := a.selInterval(v, []rune(a.lines[0]))
	if !ok || s != 4 || e != 10 {
		t.Fatalf("first row interval = %d..%d ok=%v", s, e, ok)
	}
	v = visLine{line: 1, start: 0, end: 10}
	s, e, ok = a.selInterval(v, []rune(a.lines[1]))
	if !ok || s != 0 || e != 3 {
		t.Fatalf("second row interval = %d..%d ok=%v", s, e, ok)
	}
	v = visLine{line: 0, start: 0, end: 4}
	if _, _, ok := a.selInterval(v, []rune(a.lines[0])); ok {
		t.Fatal("wrapped row before the selection start should not be highlighted")
	}
}
