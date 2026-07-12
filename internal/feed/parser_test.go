package feed

import (
	"os"
	"strings"
	"testing"
)

func TestParseOPMLReturnsNestedFeedsInDocumentOrder(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="Gaming">
      <outline text="GamesPress" title="GamesPress" description="Industry releases" xmlUrl="https://www.gamespress.com/Files/ComboRSS" type="rss"/>
      <outline text="Reddit Gaming" xmlUrl="https://www.reddit.com/r/gaming/.rss" type="rss"/>
    </outline>
    <outline text="not a feed category"/>
  </body>
</opml>`)

	got, err := ParseOPML(input)
	if err != nil {
		t.Fatalf("ParseOPML() error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ParseOPML() returned %d feeds, want 2", len(got))
	}

	if got[0].URL != "https://www.gamespress.com/Files/ComboRSS" {
		t.Errorf("first URL = %q, want GamesPress URL", got[0].URL)
	}
	if got[0].Title != "GamesPress" {
		t.Errorf("first title = %q, want %q", got[0].Title, "GamesPress")
	}
	if got[0].Description != "Industry releases" {
		t.Errorf("first description = %q, want %q", got[0].Description, "Industry releases")
	}
	if got[1].Title != "Reddit Gaming" {
		t.Errorf("second title = %q, want fallback text attribute", got[1].Title)
	}
}

func TestParseOPMLRejectsMalformedXML(t *testing.T) {
	_, err := ParseOPML([]byte(`<opml><body><outline xmlUrl="https://example.com/feed"`))
	if err == nil {
		t.Fatal("ParseOPML() error = nil, want malformed XML error")
	}
	if !strings.Contains(err.Error(), "parse OPML") {
		t.Errorf("ParseOPML() error = %q, want contextual parse error", err)
	}
}

func TestParseOPMLRejectsNonOPMLDocument(t *testing.T) {
	_, err := ParseOPML([]byte(`<rss version="2.0"><channel><title>Not OPML</title></channel></rss>`))
	if err == nil {
		t.Fatal("ParseOPML() error = nil, want non-OPML document error")
	}
	if !strings.Contains(err.Error(), "OPML root element") {
		t.Errorf("ParseOPML() error = %q, want OPML root element error", err)
	}
}

func TestParseOPMLRejectsElementsAfterRoot(t *testing.T) {
	_, err := ParseOPML([]byte(`<opml><body></body></opml><outline xmlUrl="https://example.com/feed.xml"/>`))
	if err == nil {
		t.Fatal("ParseOPML() error = nil, want malformed multi-root document error")
	}
	if !strings.Contains(err.Error(), "after OPML root") {
		t.Errorf("ParseOPML() error = %q, want after OPML root error", err)
	}
}

func TestRavenStarterOPMLParsesAllCuratedFeeds(t *testing.T) {
	input, err := os.ReadFile("../../testdata/opml/raven-feeds.opml")
	if err != nil {
		t.Fatalf("read Raven starter OPML: %v", err)
	}

	got, err := ParseOPML(input)
	if err != nil {
		t.Fatalf("ParseOPML(Raven starter OPML): %v", err)
	}
	if len(got) != 39 {
		t.Fatalf("ParseOPML(Raven starter OPML) returned %d feeds, want 39", len(got))
	}
	if got[0].URL != "https://www.gamespress.com/Files/ComboRSS" {
		t.Errorf("first Raven starter feed = %q, want GamesPress", got[0].URL)
	}
}
