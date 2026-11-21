package search

import (
	"strings"
	"unicode"
)

// Normalize foldet Kleinschreibung und ersetzt deutsche Umlaute (ä/ö/ü/ß) in eine Suchform.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	r := strings.NewReplacer(
		"ä", "ae",
		"ö", "oe",
		"ü", "ue",
		"ß", "ss",
	)
	return r.Replace(s)
}

// Tokenize extrahiert IT-spezifische Tokens (z. B. 0x8004010f, hklm\software\..., outlook.exe,
// dns_probe_finished_nxdomain) und erzeugt zusätzlich normalisierte Teil-Tokens.
func Tokenize(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	rawWords := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
		switch r {
		case '\\', '/', '_', '-', '.', ':':
			return false
		default:
			return true
		}
	})

	tokens := make([]string, 0, len(rawWords)*2)
	for _, w := range rawWords {
		w = strings.Trim(w, `\/_-.:`)
		if len(w) == 0 {
			continue
		}
		tokens = append(tokens, w)
		if norm := Normalize(w); norm != w {
			tokens = append(tokens, norm)
		}

		// Teil-Tokens bei Pfaden, Dateinamen, Bindestrichen etc.
		if strings.ContainsAny(w, `\/_-.:`) {
			parts := strings.FieldsFunc(w, func(r rune) bool {
				return r == '\\' || r == '/' || r == '_' || r == '-' || r == '.' || r == ':'
			})
			for _, p := range parts {
				if len(p) < 2 {
					continue
				}
				tokens = append(tokens, p)
				if norm := Normalize(p); norm != p {
					tokens = append(tokens, norm)
				}
			}
		}
	}
	return tokens
}

// Trigrams erzeugt 3-Zeichen-N-Gramme mit Rand-Padding für Tippfehler-Toleranz.
func Trigrams(term string) []string {
	runes := []rune(" " + term + " ")
	if len(runes) < 3 {
		return nil
	}
	out := make([]string, 0, len(runes)-2)
	for i := 0; i <= len(runes)-3; i++ {
		out = append(out, string(runes[i:i+3]))
	}
	return out
}

// EditDistance berechnet die Damerau-Levenshtein-Distanz speicherschonend mit 3 Rolling-Zeilen.
func EditDistance(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prevPrev := make([]int, lb+1)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			m := min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				if trans := prevPrev[j-2] + 1; trans < m {
					m = trans
				}
			}
			curr[j] = m
		}
		prevPrev, prev, curr = prev, curr, prevPrev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a <= b && a <= c {
		return a
	}
	if b <= c {
		return b
	}
	return c
}
