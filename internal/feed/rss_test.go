package feed

import (
	"strings"
	"testing"
)

func TestParseAtomEntriesExtractsTitleLinkAndPublishedDate(t *testing.T) {
	atom := `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Blog</title>
  <link href="https://example.com" rel="alternate"/>
  <entry>
    <title>First Post</title>
    <link href="https://example.com/first" rel="alternate"/>
    <published>2024-01-01T12:00:00Z</published>
    <updated>2024-01-02T12:00:00Z</updated>
    <id>urn:uuid:1225c695-cfb8-4ebb-aaaa-80da344efa6a</id>
    <summary>Summary of the first post.</summary>
    <author><name>Alice</name></author>
  </entry>
  <entry>
    <title>Second Post</title>
    <link href="https://example.com/second"/>
    <id>https://example.com/second</id>
  </entry>
</feed>`

	entries, feedTitle, err := ParseFeedEntries([]byte(atom))
	if err != nil {
		t.Fatalf("ParseFeedEntries() error: %v", err)
	}
	if feedTitle != "Example Blog" {
		t.Errorf("feed title = %q, want %q", feedTitle, "Example Blog")
	}
	if len(entries) != 2 {
		t.Fatalf("ParseFeedEntries() returned %d entries, want 2", len(entries))
	}
	if entries[0].Title != "First Post" {
		t.Errorf("first entry title = %q", entries[0].Title)
	}
	if entries[0].URL != "https://example.com/first" {
		t.Errorf("first entry URL = %q", entries[0].URL)
	}
	if entries[0].GUID != "urn:uuid:1225c695-cfb8-4ebb-aaaa-80da344efa6a" {
		t.Errorf("first entry GUID = %q", entries[0].GUID)
	}
	if entries[0].Author != "Alice" {
		t.Errorf("first entry author = %q, want %q", entries[0].Author, "Alice")
	}
	if !strings.Contains(entries[0].PublishedAt, "2024-01-01") {
		t.Errorf("first entry published = %q, want 2024-01-01", entries[0].PublishedAt)
	}
	if entries[1].Title != "Second Post" {
		t.Errorf("second entry title = %q", entries[1].Title)
	}
}

func TestParseRSSEntriesExtractsTitleLinkAndPublishedDate(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <link>https://example.com</link>
    <description>Test</description>
    <item>
      <title>First Post</title>
      <link>https://example.com/first</link>
      <description>Content of the first post.</description>
      <pubDate>Mon, 01 Jan 2024 12:00:00 GMT</pubDate>
      <guid isPermaLink="true">https://example.com/first</guid>
    </item>
    <item>
      <title>Second Post</title>
      <link>https://example.com/second</link>
      <pubDate>2024-01-02T12:00:00Z</pubDate>
    </item>
  </channel>
</rss>`

	entries, feedTitle, err := ParseFeedEntries([]byte(rss))
	if err != nil {
		t.Fatalf("ParseFeedEntries() error: %v", err)
	}
	if feedTitle != "Example Feed" {
		t.Errorf("feed title = %q, want %q", feedTitle, "Example Feed")
	}
	if len(entries) != 2 {
		t.Fatalf("ParseFeedEntries() returned %d entries, want 2", len(entries))
	}
	if entries[0].Title != "First Post" {
		t.Errorf("first entry title = %q", entries[0].Title)
	}
	if entries[0].URL != "https://example.com/first" {
		t.Errorf("first entry URL = %q", entries[0].URL)
	}
	if !strings.Contains(entries[0].PublishedAt, "2024-01-01") {
		t.Errorf("first entry published = %q, want 2024-01-01", entries[0].PublishedAt)
	}
	if entries[1].Title != "Second Post" {
		t.Errorf("second entry title = %q", entries[1].Title)
	}
}
