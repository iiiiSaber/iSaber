package publishing

import (
	"bytes"
	"encoding/xml"
	"html"
	"strings"
	"testing"
	"time"
)

type parsedFeed struct {
	XMLName xml.Name
	Version string `xml:"version,attr"`
	Channel struct {
		Title       string           `xml:"title"`
		Link        string           `xml:"-"`
		Description string           `xml:"description"`
		Self        parsedFeedLink   `xml:"-"`
		Links       []parsedFeedLink `xml:"link"`
		Items       []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			Category    string `xml:"category"`
			PubDate     string `xml:"pubDate"`
			GUID        struct {
				Value     string `xml:",chardata"`
				PermaLink string `xml:"isPermaLink,attr"`
			} `xml:"guid"`
		} `xml:"item"`
	} `xml:"channel"`
}

type parsedFeedLink struct {
	XMLName xml.Name
	Href    string `xml:"href,attr"`
	Rel     string `xml:"rel,attr"`
	Type    string `xml:"type,attr"`
	Value   string `xml:",chardata"`
}

func parseFeed(t *testing.T, body []byte) parsedFeed {
	t.Helper()
	var parsed parsedFeed
	if err := xml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("invalid RSS XML: %v\n%s", err, body)
	}
	if parsed.XMLName.Local != "rss" || parsed.Version != "2.0" {
		t.Fatal("missing RSS version")
	}
	for _, link := range parsed.Channel.Links {
		if link.XMLName.Space == "" {
			parsed.Channel.Link = link.Value
		}
		if link.XMLName.Space == "http://www.w3.org/2005/Atom" {
			parsed.Channel.Self = link
		}
	}
	return parsed
}
func TestFeedXMLSerialization(t *testing.T) {
	published := time.Date(2026, 11, 26, 12, 0, 0, 0, time.FixedZone("China", 8*3600))
	summary := `中文 & <script>alert("x")</script> 'quote' ]]> 😀`
	entries := []feedEntry{{ID: "11111111-1111-4111-8111-111111111111", Slug: "test-entry", Title: "English & 中文 😀\x00\uFFFE", Summary: summary, Category: "Go & 中文", PublishedAt: published}, {ID: "22222222-2222-4222-8222-222222222222", Slug: "no-summary", Title: "Empty summary", PublishedAt: published}}
	body, err := renderFeed("https://blog.example/", entries)
	if err != nil {
		t.Fatal(err)
	}
	parsed := parseFeed(t, body)
	if parsed.Channel.Title != "iSaber" || parsed.Channel.Link != "https://blog.example/" || parsed.Channel.Self.Href != "https://blog.example/feed.xml" || parsed.Channel.Self.Rel != "self" || parsed.Channel.Self.Type != "application/rss+xml" {
		t.Fatalf("wrong channel metadata: %#v\n%s", parsed.Channel, body)
	}
	item := parsed.Channel.Items[0]
	if item.Title != "English & 中文 😀" || html.UnescapeString(item.Description) != summary || strings.Contains(item.Description, "<script>") || item.Category != "Go & 中文" {
		t.Fatalf("text did not survive safely: %#v", item)
	}
	if item.GUID.PermaLink != "false" || item.GUID.Value != "urn:uuid:"+entries[0].ID || item.Link != "https://blog.example/articles/test-entry" || item.PubDate != "Thu, 26 Nov 2026 04:00:00 GMT" {
		t.Fatalf("wrong item metadata: %#v", item)
	}
	if parsed.Channel.Items[1].Description != "" || parsed.Channel.Items[1].Category != "" || bytes.Count(body, []byte("<description>")) != 2 {
		t.Fatal("optional fields not omitted")
	}
	again, _ := renderFeed("https://blog.example/", entries)
	if !bytes.Equal(body, again) {
		t.Fatal("unstable XML")
	}
	empty, err := renderFeed("http://127.0.0.1:3000", nil)
	if err != nil || len(parseFeed(t, empty).Channel.Items) != 0 {
		t.Fatal("empty feed is not valid")
	}
}
func TestFeedCanonicalOriginValidation(t *testing.T) {
	for _, origin := range []string{"", "https://user:secret@blog.example", "http://blog.example", "https://blog.example/path", "https://blog.example?x=1", "https://blog.example?", "https://blog.example#fragment", "javascript:alert(1)"} {
		if _, err := renderFeed(origin, nil); err == nil {
			t.Fatalf("invalid feed origin accepted: %s", origin)
		}
	}
}
func TestFeedConditionalValidators(t *testing.T) {
	current := `W/"current"`
	cases := []struct {
		header string
		match  bool
	}{
		{``, false}, {`*`, true}, {`W/"current"`, true}, {`"current"`, true}, {`"other", W/"current"`, true},
		{`W/"other,with,commas", "current"`, true}, {`W/"missing"`, false}, {`"current-with-suffix"`, false},
		{`current`, false}, {`"current"garbage`, false}, {`"current",bad`, false}, {`"unterminated`, false}, {`*, "current"`, false},
	}
	for _, c := range cases {
		if got := feedETagMatches(c.header, current); got != c.match {
			t.Errorf("%q: got %v want %v", c.header, got, c.match)
		}
	}
}
