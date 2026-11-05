//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type wbiKeys struct {
	imgKey   string
	subKey   string
	mixinKey string
}

// mixinKeyEncTab is the fixed permutation published by bilibili's web client.
var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49,
	33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40,
	61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11,
	36, 20, 34, 44, 52,
}

func getMixinKey(orig string) string {
	if len(orig) < 64 {
		return ""
	}
	var b strings.Builder
	for _, i := range mixinKeyEncTab {
		b.WriteByte(orig[i])
	}
	s := b.String()
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

// signParams adds wts and w_rid in place. Values keep bilibili's filter rules.
func signParams(params url.Values, mixinKey string, ts int64) {
	params.Set("wts", strconv.FormatInt(ts, 10))
	keys := make([]string, 0, len(params))
	for k := range params {
		if k != "w_rid" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strings.Map(func(r rune) rune {
			switch r {
			case '!', '\'', '(', ')', '*':
				return -1
			}
			return r
		}, params.Get(k)))
	}
	sum := md5.Sum([]byte(b.String() + mixinKey))
	params.Set("w_rid", hex.EncodeToString(sum[:]))
}

func wbiFileName(rawURL string) string {
	s := rawURL
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, "."); i >= 0 {
		s = s[:i]
	}
	return s
}

func parseWBIKeys(imgURL, subURL string) *wbiKeys {
	img := wbiFileName(imgURL)
	sub := wbiFileName(subURL)
	if len(img) < 32 || len(sub) < 32 {
		return nil
	}
	mixin := getMixinKey(img + sub)
	if mixin == "" {
		return nil
	}
	return &wbiKeys{imgKey: img, subKey: sub, mixinKey: mixin}
}
