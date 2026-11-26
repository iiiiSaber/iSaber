package publishing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const feedCacheControl = "no-cache, max-age=0, must-revalidate"

type feedEntry struct {
	ID, Slug, Title, Summary, Category string
	PublishedAt                        time.Time
}
type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	AtomNS  string     `xml:"xmlns:atom,attr"`
	Channel rssChannel `xml:"channel"`
}
type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Self        rssSelf   `xml:"atom:link"`
	Items       []rssItem `xml:"item"`
}
type rssSelf struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}
type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	Description string  `xml:"description,omitempty"`
	PubDate     string  `xml:"pubDate"`
	Category    string  `xml:"category,omitempty"`
}
type rssGUID struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

// RSS never uses ownerReading: owner cookies must also receive only public data.
// One statement gives selection, published metadata and category names the same snapshot.
func (s *Service) feed(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT a.id,a.slug,COALESCE(r.title,''),
 COALESCE(r.summary,''),a.published_at,COALESCE(c.name,'')
 FROM articles a JOIN revisions r ON r.id=a.published_revision_id AND r.article_id=a.id
 LEFT JOIN categories c ON c.id=r.category_id
 WHERE a.visibility='public' AND a.published_revision_id IS NOT NULL
 ORDER BY a.published_at DESC,a.id DESC LIMIT 20`)
	if err != nil {
		feedUnavailable(w, r)
		return
	}
	defer rows.Close()
	entries := make([]feedEntry, 0, 20)
	for rows.Next() {
		var item feedEntry
		if err := rows.Scan(&item.ID, &item.Slug, &item.Title, &item.Summary, &item.PublishedAt, &item.Category); err != nil {
			feedUnavailable(w, r)
			return
		}
		entries = append(entries, item)
	}
	if rows.Err() != nil {
		feedUnavailable(w, r)
		return
	}
	// commentOrigin is the validated SITE_ORIGIN shared by public publishing features.
	body, err := renderFeed(s.commentOrigin, entries)
	if err != nil {
		feedUnavailable(w, r)
		return
	}
	sum := sha256.Sum256(body)
	etag := `W/"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", feedCacheControl)
	w.Header().Set("ETag", etag)
	// Visibility is re-evaluated before comparing validators, including on HEAD.
	if feedETagMatches(strings.Join(r.Header.Values("If-None-Match"), ","), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
func feedUnavailable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "60")
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	reply(w, nil, fault(http.StatusServiceUnavailable, "feed_unavailable"))
}
func renderFeed(origin string, entries []feedEntry) ([]byte, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid feed origin")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return nil, errors.New("invalid feed origin scheme")
	}
	base := u.Scheme + "://" + u.Host
	document := rssDocument{Version: "2.0", AtomNS: "http://www.w3.org/2005/Atom", Channel: rssChannel{
		Title: "iSaber", Link: base + "/", Description: "Personal journals, technical articles, and things I make. English and 中文.",
		Self: rssSelf{Href: base + "/feed.xml", Rel: "self", Type: "application/rss+xml"},
	}}
	for _, entry := range entries {
		document.Channel.Items = append(document.Channel.Items, rssItem{
			Title: xmlText(entry.Title), Link: base + "/articles/" + url.PathEscape(entry.Slug), GUID: rssGUID{Value: "urn:uuid:" + entry.ID},
			// Readers may interpret description as HTML after decoding XML. Escape both layers.
			Description: html.EscapeString(xmlText(entry.Summary)), PubDate: entry.PublishedAt.UTC().Format(http.TimeFormat), Category: xmlText(entry.Category),
		})
	}
	body, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}
func xmlText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF) {
			return r
		}
		return -1
	}, value)
}

// If-None-Match uses weak comparison. Opaque quoted tags may contain commas.
func feedETagMatches(value, current string) bool {
	value = strings.TrimSpace(value)
	if value == "*" {
		return true
	}
	matched := false
	for value != "" {
		value = strings.TrimSpace(strings.TrimPrefix(value, "W/"))
		if len(value) < 2 || value[0] != '"' {
			return false
		}
		end := strings.IndexByte(value[1:], '"') + 1
		if end == 0 {
			return false
		}
		matched = matched || value[:end+1] == strings.TrimPrefix(current, "W/")
		value = strings.TrimSpace(value[end+1:])
		if value != "" {
			if value[0] != ',' {
				return false
			}
			value = strings.TrimSpace(value[1:])
		}
	}
	return matched
}
