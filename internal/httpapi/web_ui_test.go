package httpapi

import (
	"bytes"
	"testing"

	"golang.org/x/net/html"
)

func TestWebNavigationTargetsAreDistinctPages(t *testing.T) {
	data, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	wanted := map[string]bool{
		"dashboard": false,
		"library":   false,
		"downloads": false,
		"runs":      false,
		"settings":  false,
	}
	navigable := map[string]bool{
		"dashboard": false,
		"library":   false,
		"downloads": false,
		"runs":      false,
		"settings":  false,
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "button" {
			if class, ok := htmlAttr(n, "class"); ok && class == "nav-item" {
				if view, ok := htmlAttr(n, "data-view"); ok {
					if _, exists := navigable[view]; exists {
						navigable[view] = true
					}
				}
			}
		}
		if n.Type != html.ElementNode || n.Data != "section" {
			return
		}
		if id, ok := htmlAttr(n, "id"); ok {
			for view := range wanted {
				if id == "view-"+view {
					wanted[view] = true
				}
			}
		}
	})
	for view, exists := range wanted {
		if !exists {
			t.Errorf("missing independent page for %q", view)
		}
	}
	for view, exists := range navigable {
		if !exists {
			t.Errorf("missing primary navigation entry for %q", view)
		}
	}
}

func TestMobileSourceSelectorHasAccessibleLabel(t *testing.T) {
	data, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	var selector *html.Node
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "select" {
			if id, ok := htmlAttr(n, "id"); ok && id == "mobile-source-select" {
				selector = n
			}
		}
	})
	if selector == nil {
		t.Fatal("missing mobile source selector")
	}
	if label, ok := htmlAttr(selector, "aria-label"); !ok || label == "" {
		t.Fatal("mobile source selector must have an accessible label")
	}
}

func walkHTML(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}

func htmlAttr(n *html.Node, name string) (string, bool) {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val, true
		}
	}
	return "", false
}
