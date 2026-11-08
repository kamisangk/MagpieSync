//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type identityTransport func(*http.Request) (*http.Response, error)

func (f identityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVideoAPIsAcceptArchiveAndBVIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		id   string
		keys map[string]string
	}{
		{"117370534364338", map[string]string{
			"/x/web-interface/view":            "aid",
			"/x/web-interface/view/detail/tag": "aid",
			"/x/player/wbi/playurl":            "avid",
			"/x/player/wbi/v2":                 "aid",
		}},
		{"av117370534364338", map[string]string{
			"/x/web-interface/view":            "aid",
			"/x/web-interface/view/detail/tag": "aid",
			"/x/player/wbi/playurl":            "avid",
			"/x/player/wbi/v2":                 "aid",
		}},
		{"BV11pa26dEit", map[string]string{
			"/x/web-interface/view":            "bvid",
			"/x/web-interface/view/detail/tag": "bvid",
			"/x/player/wbi/playurl":            "bvid",
			"/x/player/wbi/v2":                 "bvid",
		}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			c := NewGuest()
			c.wbi = &wbiKeys{mixinKey: "test"}
			c.http.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
				q := r.URL.Query()
				key := tc.keys[r.URL.Path]
				want := tc.id
				if strings.HasPrefix(strings.ToLower(want), "av") {
					want = want[2:]
				}
				if key == "" || q.Get(key) != want {
					t.Errorf("%s query=%s; want %s=%s", r.URL.Path, q.Encode(), key, want)
				}
				for _, other := range []string{"aid", "avid", "bvid"} {
					if other != key && q.Get(other) != "" {
						t.Errorf("unexpected %s in %s query", other, r.URL.Path)
					}
				}
				data := `{"code":0,"data":{"bvid":"BV11pa26dEit","pages":[{"cid":123,"page":1,"part":"test"}]}}`
				switch r.URL.Path {
				case "/x/web-interface/view/detail/tag":
					data = `{"code":0,"data":[]}`
				case "/x/player/wbi/playurl":
					data = `{"code":0,"data":{"quality":80,"durl":[{"url":"https://example.test/video.mp4"}]}}`
				case "/x/player/wbi/v2":
					data = `{"code":0,"data":{}}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(data)), Request: r}, nil
			})
			ctx := context.Background()
			if _, err := c.VideoDetail(ctx, tc.id); err != nil {
				t.Fatal(err)
			}
			if _, err := c.VideoTags(ctx, tc.id); err != nil {
				t.Fatal(err)
			}
			if _, err := c.PlayURL(ctx, tc.id, 123, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Subtitles(ctx, tc.id, 123); err != nil {
				t.Fatal(err)
			}
		})
	}
}
