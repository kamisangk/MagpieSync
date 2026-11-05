//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"strings"
	"testing"
)

func pbVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func pbField(field int, wire int, payload []byte) []byte {
	out := pbVarint(uint64(field<<3 | wire))
	if wire == 2 {
		out = append(out, pbVarint(uint64(len(payload)))...)
	}
	return append(out, payload...)
}

func TestDecodeDanmakuSegment(t *testing.T) {
	elem := append(pbField(1, 0, pbVarint(42)), pbField(2, 0, pbVarint(1500))...)
	elem = append(elem, pbField(3, 0, pbVarint(1))...)
	elem = append(elem, pbField(4, 0, pbVarint(25))...)
	elem = append(elem, pbField(5, 0, pbVarint(0xffffff))...)
	elem = append(elem, pbField(7, 2, []byte("hello"))...)
	body := pbField(1, 2, elem)

	items, err := decodeDanmakuSegment(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Progress != 1500 || items[0].Content != "hello" || items[0].Mode != 1 || items[0].FontSize != 25 {
		t.Fatalf("danmaku = %+v", items[0])
	}
}

func TestDanmakuASS(t *testing.T) {
	out := string(DanmakuASS([]Danmaku{
		{Progress: 1000, Mode: 1, FontSize: 25, Color: 0xffffff, Content: "hello"},
		{Progress: 2000, Mode: 5, FontSize: 25, Color: 0xff0000, Content: "top"},
		{Progress: 3000, Mode: 4, FontSize: 25, Color: 0x00ff00, Content: "bottom"},
	}, 1920, 1080))
	for _, want := range []string{"[Script Info]", "PlayResX: 1920", "Dialogue:", "\\move(", "\\pos(", "top", "bottom"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ASS missing %q:\n%s", want, out)
		}
	}
}

func TestSubtitleSRT(t *testing.T) {
	out := string(SubtitleSRT([]SubtitleItem{
		{From: 1.5, To: 2.25, Content: "第一句"},
		{From: 3, To: 4.001, Content: "第二句"},
	}))
	if !strings.Contains(out, "00:00:01,500 --> 00:00:02,250") {
		t.Fatalf("SRT time wrong:\n%s", out)
	}
	if !strings.Contains(out, "2\n00:00:03,000 --> 00:00:04,001") {
		t.Fatalf("SRT second item wrong:\n%s", out)
	}
}
