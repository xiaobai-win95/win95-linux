package markdown

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func parseInlines(text string, depth int) ([]Inline, error) {
	if depth > maxNesting {
		return nil, fmt.Errorf("markdown: inline nesting exceeds %d levels", maxNesting)
	}
	var out []Inline
	var literal strings.Builder
	flush := func() {
		if literal.Len() == 0 {
			return
		}
		appendInlineText(&out, literal.String())
		literal.Reset()
	}
	appendNode := func(node Inline) {
		flush()
		out = append(out, node)
	}

	for i := 0; i < len(text); {
		ch := text[i]
		switch ch {
		case '\\':
			if i+1 >= len(text) {
				literal.WriteByte(ch)
				i++
				continue
			}
			next := text[i+1]
			switch {
			case next == '\n':
				appendNode(Inline{Kind: InlineBreak})
				i += 2
			case next == '(':
				end := findMathEnd(text, i+2, `\)`)
				if end > i+2 && validMath(text[i+2:end]) {
					appendNode(Inline{Kind: InlineMath, Text: text[i+2 : end]})
					i = end + 2
				} else {
					literal.WriteByte(ch)
					i++
				}
			case isASCIIPunctuation(next):
				literal.WriteByte(next)
				i += 2
			default:
				literal.WriteByte(ch)
				i++
			}
		case '`':
			n := backtickRun(text, i)
			if end := findBacktickClose(text, i+n, n); end >= 0 {
				code := strings.ReplaceAll(text[i+n:end], "\n", " ")
				if len(code) >= 2 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.TrimSpace(code) != "" {
					code = code[1 : len(code)-1]
				}
				appendNode(Inline{Kind: InlineCode, Text: code})
				i = end + n
			} else {
				literal.WriteString(text[i : i+n])
				i += n
			}
		case '$':
			if i+1 < len(text) && text[i+1] == '$' {
				literal.WriteString("$$")
				i += 2
				continue
			}
			if end := findMathEnd(text, i+1, "$"); end > i+1 && validMath(text[i+1:end]) {
				appendNode(Inline{Kind: InlineMath, Text: text[i+1 : end]})
				i = end + 1
			} else {
				literal.WriteByte(ch)
				i++
			}
		case '[':
			if closeLabel := findLabelEnd(text, i+1); closeLabel >= 0 && closeLabel+1 < len(text) && text[closeLabel+1] == '(' {
				if closeURL := findURLend(text, closeLabel+2); closeURL >= 0 {
					children, err := parseInlines(text[i+1:closeLabel], depth+1)
					if err != nil {
						return nil, err
					}
					url, _ := linkTarget(text[closeLabel+2 : closeURL])
					appendNode(Inline{Kind: InlineLink, URL: url, Children: children})
					i = closeURL + 1
					continue
				}
			}
			literal.WriteByte(ch)
			i++
		case '<':
			if end := strings.IndexByte(text[i+1:], '>'); end >= 0 {
				end += i + 1
				content := text[i+1 : end]
				if !strings.ContainsAny(content, "\n\r ") && isAutoLink(content) {
					url := content
					if !strings.Contains(content, ":") {
						url = "mailto:" + content
					}
					appendNode(Inline{Kind: InlineLink, URL: url, Children: []Inline{{Kind: InlineText, Text: content}}})
					i = end + 1
					continue
				}
			}
			literal.WriteByte(ch)
			i++
		case '*', '_':
			node, end, ok, err := parseEmphasis(text, i, depth)
			if err != nil {
				return nil, err
			}
			if ok {
				appendNode(node)
				i = end
			} else {
				literal.WriteByte(ch)
				i++
			}
		case ' ', '\t':
			j := i + 1
			for j < len(text) && (text[j] == ' ' || text[j] == '\t') {
				j++
			}
			if j < len(text) && text[j] == '\n' {
				flush()
				if j-i >= 2 {
					out = append(out, Inline{Kind: InlineBreak})
				} else {
					appendInlineText(&out, " ")
				}
				i = j + 1
			} else {
				literal.WriteByte(' ')
				i = j
			}
		case '\n':
			flush()
			appendInlineText(&out, " ")
			i++
		default:
			r, size := utf8.DecodeRuneInString(text[i:])
			literal.WriteRune(r)
			i += size
		}
	}
	flush()
	return out, nil
}

func appendInlineText(out *[]Inline, text string) {
	if text == "" {
		return
	}
	if len(*out) > 0 && (*out)[len(*out)-1].Kind == InlineText {
		(*out)[len(*out)-1].Text += text
		return
	}
	*out = append(*out, Inline{Kind: InlineText, Text: text})
}

func isASCIIPunctuation(ch byte) bool {
	return strings.ContainsRune(`!"#$%&'()*+,-./:;<=>?@[\]^_`+"`"+`{|}~`, rune(ch))
}

func backtickRun(s string, pos int) int {
	end := pos + 1
	for end < len(s) && s[end] == '`' {
		end++
	}
	return end - pos
}

func findBacktickClose(s string, from, width int) int {
	for i := from; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		n := backtickRun(s, i)
		if n == width {
			return i
		}
		i += n
	}
	return -1
}

func findMathEnd(s string, from int, close string) int {
	for i := from; i < len(s); {
		if s[i] == '\n' {
			return -1
		}
		if strings.HasPrefix(s[i:], close) {
			return i
		}
		if s[i] == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return -1
}

func findLabelEnd(s string, from int) int {
	depth, ticks := 1, 0
	for i := from; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if s[i] == '`' {
			n := backtickRun(s, i)
			if ticks == 0 {
				ticks = n
			} else if ticks == n {
				ticks = 0
			}
			i += n
			continue
		}
		if ticks == 0 {
			switch s[i] {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					return i
				}
			}
		}
		i++
	}
	return -1
}

func findURLend(s string, from int) int {
	depth, angle := 0, false
	for i := from; i < len(s); {
		ch := s[i]
		if ch == '\n' || ch == '\r' {
			return -1
		}
		if ch == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if angle {
			if ch == '>' {
				angle = false
			}
			i++
			continue
		}
		switch ch {
		case '<':
			angle = true
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
		i++
	}
	return -1
}

func linkTarget(s string) (url, title string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if s[0] == '<' {
		if end := strings.IndexByte(s, '>'); end >= 0 {
			return s[1:end], strings.TrimSpace(s[end+1:])
		}
	}
	for i, r := range s {
		if unicode.IsSpace(r) {
			return s[:i], strings.TrimSpace(s[i:])
		}
	}
	return s, ""
}

func isAutoLink(s string) bool {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "mailto:") {
		return len(s) > len("http://")
	}
	return strings.Contains(s, "@") && strings.Contains(s, ".") && !strings.HasPrefix(s, "@")
}

func parseEmphasis(s string, start, depth int) (Inline, int, bool, error) {
	marker := s[start]
	run := 1
	for start+run < len(s) && s[start+run] == marker {
		run++
	}
	width := run
	if width > 3 {
		width = 3
	}
	delim := strings.Repeat(string(marker), width)
	after := start + width
	if after >= len(s) {
		return Inline{}, 0, false, nil
	}
	next, _ := utf8.DecodeRuneInString(s[after:])
	if unicode.IsSpace(next) {
		return Inline{}, 0, false, nil
	}
	prev := rune(0)
	if start > 0 {
		prev, _ = utf8.DecodeLastRuneInString(s[:start])
	}
	if marker == '_' && isAlphaNumeric(prev) && isAlphaNumeric(next) {
		return Inline{}, 0, false, nil
	}
	end := findEmphasisClose(s, after, delim, marker)
	if end < 0 {
		return Inline{}, 0, false, nil
	}
	children, err := parseInlines(s[after:end], depth+1)
	if err != nil {
		return Inline{}, 0, false, err
	}
	var node Inline
	switch width {
	case 3:
		node = Inline{Kind: InlineBold, Children: []Inline{{Kind: InlineItalic, Children: children}}}
	case 2:
		node = Inline{Kind: InlineBold, Children: children}
	default:
		node = Inline{Kind: InlineItalic, Children: children}
	}
	return node, end + width, true, nil
}

func findEmphasisClose(s string, from int, delim string, marker byte) int {
	for i := from; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if s[i] == '`' {
			n := backtickRun(s, i)
			if end := findBacktickClose(s, i+n, n); end >= 0 {
				i = end + n
				continue
			}
			i += n
			continue
		}
		if s[i] == marker {
			n := 1
			for i+n < len(s) && s[i+n] == marker {
				n++
			}
			if n == len(delim) {
				prev, _ := utf8.DecodeLastRuneInString(s[:i])
				if !unicode.IsSpace(prev) {
					next := rune(0)
					if i+n < len(s) {
						next, _ = utf8.DecodeRuneInString(s[i+n:])
					}
					if marker != '_' || !isAlphaNumeric(prev) || !isAlphaNumeric(next) {
						return i
					}
				}
			}
			i += n
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return -1
}

func isAlphaNumeric(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}
