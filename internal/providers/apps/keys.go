// keys.go expands an AppEntry into every string a query may match
// against, including pinyin transcriptions of Han-character names.
package apps

import (
	"strings"

	"github.com/mozillazg/go-pinyin"

	"kyvro/internal/core"
)

var (
	pyNormal = pinyin.NewArgs() // Style Normal is the zero value
	pyFirst  = pinyin.NewArgs()
)

func init() {
	pyFirst.Style = pinyin.FirstLetter
}

// searchKeys returns the match targets for the entry: the display name,
// its alternate (un-localized) names, and — for names containing Han
// characters — the full pinyin and initial-letter pinyin transcriptions
// (钉钉 → dingding / dd). Non-Han runes pass through verbatim, so
// 微信Mac → weixinMac / wxMac.
func searchKeys(e core.AppEntry) []string {
	keys := make([]string, 0, 1+2*len(e.AltNames)+2*2)
	add := func(k string) {
		if k == "" {
			return
		}
		for _, existing := range keys {
			if strings.EqualFold(existing, k) {
				return
			}
		}
		keys = append(keys, k)
	}
	add(e.Name)
	for _, alt := range e.AltNames {
		add(alt)
	}
	base := append([]string(nil), keys...)
	for _, k := range base {
		if full, initials, ok := pinyinOf(k); ok {
			add(full)
			add(initials)
		}
	}
	return keys
}

// pinyinOf transliterates the Han runes of s (keeping the rest verbatim)
// into full pinyin and initial letters; ok is false when s has no Han
// characters.
func pinyinOf(s string) (full, initials string, ok bool) {
	hasHan := false
	for _, r := range s {
		if isHan(r) {
			hasHan = true
			break
		}
	}
	if !hasHan {
		return "", "", false
	}
	var f, i strings.Builder
	for _, r := range s {
		if !isHan(r) {
			f.WriteRune(r)
			i.WriteRune(r)
			continue
		}
		f.WriteString(pinyin.SinglePinyin(r, pyNormal)[0])
		i.WriteString(pinyin.SinglePinyin(r, pyFirst)[0])
	}
	return f.String(), i.String(), true
}

// isHan reports whether r is a CJK Unified Ideograph.
func isHan(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}
