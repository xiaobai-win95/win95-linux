package desktop

import (
	"fmt"
	"testing"
)

func TestSolitaireFairOpening(t *testing.T) {
	d := testDesktop(t)
	a := &SolitaireApp{d: d}
	signatures := map[string]bool{}
	for i := 0; i < 150; i++ {
		a.newGame()
		if got := a.solOpeningMoves(); got < 1 {
			t.Fatalf("deal %d has no opening move (hell opening)", i)
		}
		if len(a.stock) != 24 {
			t.Fatalf("deal %d stock = %d, want 24", i, len(a.stock))
		}
		seen := map[[2]int]bool{}
		for _, c := range a.stock {
			seen[[2]int{c.suit, c.rank}] = true
		}
		sig := ""
		for col := 0; col < 7; col++ {
			if len(a.tab[col]) != col+1 {
				t.Fatalf("deal %d column %d has %d cards", i, col, len(a.tab[col]))
			}
			for _, c := range a.tab[col] {
				seen[[2]int{c.suit, c.rank}] = true
			}
			top := a.tab[col][len(a.tab[col])-1]
			sig += fmt.Sprintf("%d.%d/", top.suit, top.rank)
		}
		if len(seen) != 52 {
			t.Fatalf("deal %d has %d unique cards, want 52", i, len(seen))
		}
		signatures[sig] = true
	}
	if len(signatures) < 100 {
		t.Fatalf("only %d distinct deals in 150 games, shuffle looks broken", len(signatures))
	}
}

func TestSolitaireFirstPassScore(t *testing.T) {
	d := testDesktop(t)
	a := &SolitaireApp{d: d}
	a.dealRandom()
	// Stock of exactly 3: the only waste top is stock[0].
	a.stock = []solCard{
		{suit: solHearts, rank: 1},
		{suit: solClubs, rank: 9},
		{suit: solDiamonds, rank: 5},
	}
	for col := range a.tab {
		a.tab[col] = nil
	}
	if got := a.solFirstPassScore(); got != 2 {
		t.Fatalf("ace waste top score = %d, want 2", got)
	}
	// 4 cards: first waste top stock[1], partial group top stock[0].
	a.stock = []solCard{
		{suit: solSpades, rank: 1},
		{suit: solHearts, rank: 1},
		{suit: solClubs, rank: 9},
		{suit: solDiamonds, rank: 5},
	}
	if got := a.solFirstPassScore(); got != 4 {
		t.Fatalf("two ace waste tops score = %d, want 4", got)
	}
}