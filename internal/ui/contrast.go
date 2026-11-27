package ui

import "math"

func luminance(col uint32) float64 {
	r := linear(float64((col>>16)&0xff) / 255)
	g := linear(float64((col>>8)&0xff) / 255)
	b := linear(float64(col&0xff) / 255)
	return 0.2126*r + 0.7152*g + 0.0722*b
}

func linear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func Contrast(a, b uint32) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func ReadableText(bg uint32) uint32 {
	if Contrast(ColBlack, bg) >= Contrast(ColWhite, bg) {
		return ColBlack
	}
	return ColWhite
}

func EnsureContrast(fg, bg uint32, minimum float64) uint32 {
	if Contrast(fg, bg) >= minimum {
		return fg
	}
	if Contrast(ColBlack, bg) > Contrast(ColWhite, bg) {
		return ColBlack
	}
	return ColWhite
}

func TextOn(col uint32) uint32 { return EnsureContrast(ColText, col, 4.5) }

func EnsureContrastPair(fg uint32, backgrounds ...uint32) uint32 {
	ok := true
	for _, bg := range backgrounds {
		if Contrast(fg, bg) < 4.5 {
			ok = false
			break
		}
	}
	if ok {
		return fg
	}
	best, bestRatio := fg, -1.0
	for _, candidate := range []uint32{ColBlack, ColWhite} {
		ratio := math.Inf(1)
		for _, bg := range backgrounds {
			ratio = math.Min(ratio, Contrast(candidate, bg))
		}
		if ratio > bestRatio {
			best, bestRatio = candidate, ratio
		}
	}
	return best
}
