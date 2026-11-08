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
	for _, tc := range []struct{ id, key, value string }{
		{"117370534364338", "aid", "117370534364338"},
		{"av117370534364338", "aid", "117370534364338"},
		{"BV11pa26dEit", "bvid", "BV11pa26dEit"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			c := NewGuest()
			c.wbi = &wbiKeys{mixinKey: "test"}
			c.http.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
				q := r.URL.Query()
				if q.Get(tc.key) != tc.value {
					t.Errorf("%s query=%s; want %s=%s", r.URL.Path, q.Encode(), tc.key, tc.value)
				}
				other := "bvid"
				if tc.key == "bvid" {
					other = "aid"
				}
				if q.Get(other) != "" {
					t.Errorf("unexpected %s", other)
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
