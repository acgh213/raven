package handler

import (
	"strings"
	"testing"
)

func TestIsRedditURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://www.reddit.com/r/golang/comments/abc/title/", true},
		{"https://old.reddit.com/r/programming/comments/xyz/foo/", true},
		{"https://redd.it/abc123", true},
		{"https://github.com/golang/go", false},
		{"https://example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isRedditURL(tt.url)
		if got != tt.want {
			t.Errorf("isRedditURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestRedditJSONURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{
			"https://www.reddit.com/r/golang/comments/abc123/title/",
			"https://www.reddit.com/r/golang/comments/abc123/title.json",
		},
		{
			"https://www.reddit.com/r/golang/comments/abc123/title/?utm_source=foo",
			"https://www.reddit.com/r/golang/comments/abc123/title.json",
		},
		{
			"https://old.reddit.com/r/programming/comments/xyz/foo/",
			"https://www.reddit.com/r/programming/comments/xyz/foo.json",
		},
		{
			"https://redd.it/abc123",
			"https://www.reddit.com/abc123.json",
		},
	}
	for _, tt := range tests {
		got, err := redditJSONURL(tt.in)
		if err != nil {
			t.Errorf("redditJSONURL(%q) error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("redditJSONURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseRedditJSON(t *testing.T) {
	body := []byte(`[{"data":{"children":[{"data":{"selftext":"Hello world","selftext_html":"&lt;p&gt;Hello world&lt;/p&gt;","title":"Test Post","author":"testuser","permalink":"/r/test/comments/abc/test/"}}]}}]`)
	text, author, permalink, err := parseRedditJSON(body)
	if err != nil {
		t.Fatalf("parseRedditJSON: %v", err)
	}
	if text != "Hello world" {
		t.Errorf("text = %q, want %q", text, "Hello world")
	}
	if author != "testuser" {
		t.Errorf("author = %q, want %q", author, "testuser")
	}
	if permalink != "/r/test/comments/abc/test/" {
		t.Errorf("permalink = %q", permalink)
	}
}

func TestParseRedditJSONFallsBackToHTML(t *testing.T) {
	// selftext is empty, selftext_html has content
	body := []byte(`[{"data":{"children":[{"data":{"selftext":"","selftext_html":"&lt;p&gt;HTML content&lt;/p&gt;","title":"Test","author":"u","permalink":"/r/t/comments/1/test/"}}]}}]`)
	text, _, _, err := parseRedditJSON(body)
	if err != nil {
		t.Fatalf("parseRedditJSON: %v", err)
	}
	if !strings.Contains(text, "HTML content") {
		t.Errorf("text = %q, want it to contain 'HTML content'", text)
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"<p>Hello</p>", "Hello"},
		{"<a href='foo'>click</a> here", "click here"},
		{"&lt;div&gt;", "<div>"},
		{"&amp; &lt; &gt; &quot; &#39;", "& < > \" '"},
		{"plain text", "plain text"},
	}
	for _, tt := range tests {
		got := stripHTML(tt.in)
		if got != tt.want {
			t.Errorf("stripHTML(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
