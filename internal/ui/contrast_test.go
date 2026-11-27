package ui

import "testing"

func TestPaletteTextContrast(t *testing.T) {
	for _, p := range Palettes {
		ApplyPalette(p)
		for name, bg := range map[string]uint32{"face": ColFace, "field": ColField} {
			if got := Contrast(ColText, bg); got < 4.5 {
				t.Errorf("%s text on %s contrast %.2f", p.Name, name, got)
			}
		}
	}
	ApplyPalette(Palettes[0])
}

func TestEnsureContrastPair(t *testing.T) {
	got := EnsureContrastPair(0x808080, 0x000000, 0x1c2438)
	if Contrast(got, 0x000000) < 4.5 || Contrast(got, 0x1c2438) < 4.5 {
		t.Fatalf("unable to produce readable pair: %06x", got)
	}
}
