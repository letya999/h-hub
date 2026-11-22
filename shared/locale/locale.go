// Package locale renders user-visible text from ru/en catalogs by key.
// Tests assert catalog parity and render expected text through Lookup,
// never through duplicated literals.
package locale

import (
	"fmt"
	"strings"
)

// Lang is a supported UI language.
type Lang string

const (
	RU Lang = "ru"
	EN Lang = "en"
)

// Lookup resolves a catalog key for a language. The boolean reports
// whether the key exists.
func Lookup(lang Lang, key string) (string, bool) {
	cat, ok := catalogs[lang]
	if !ok {
		return "", false
	}
	s, ok := cat[key]
	return s, ok
}

// T renders a key with {name} placeholders replaced by args taken as
// name/value pairs. A missing key echoes the key so failures are
// visible in tests.
func T(lang Lang, key string, args ...any) string {
	s, ok := Lookup(lang, key)
	if !ok {
		return key
	}
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok {
			continue
		}
		s = strings.ReplaceAll(s, "{"+name+"}", fmt.Sprint(args[i+1]))
	}
	return s
}

// Langs lists every language the catalogs cover.
func Langs() []Lang {
	out := make([]Lang, 0, len(catalogs))
	for l := range catalogs {
		out = append(out, l)
	}
	return out
}

// Catalog exposes one language's key set for parity tests.
func Catalog(lang Lang) map[string]string {
	cat, ok := catalogs[lang]
	if !ok {
		return nil
	}
	out := make(map[string]string, len(cat))
	for k, v := range cat {
		out[k] = v
	}
	return out
}
