package handler

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// redditPost is the post data from Reddit's .json API.
type redditPost struct {
	Data struct {
		Children []struct {
			Data struct {
				Selftext     string `json:"selftext"`
				SelftextHTML string `json:"selftext_html"`
				Title        string `json:"title"`
				Author       string `json:"author"`
				Permalink    string `json:"permalink"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// isRedditURL returns true if url is a Reddit post page.
func isRedditURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.HasSuffix(u.Host, "reddit.com") ||
		strings.HasSuffix(u.Host, "redd.it") ||
		u.Host == "old.reddit.com"
}

// redditJSONURL converts a Reddit post URL to its .json API endpoint.
// e.g. https://www.reddit.com/r/golang/comments/abc123/title/?utm=foo
// → https://www.reddit.com/r/golang/comments/abc123/title/.json
func redditJSONURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse reddit url: %w", err)
	}

	// Normalise to www.reddit.com so the API responds consistently.
	u.Host = "www.reddit.com"

	// Strip everything after the post slug and append .json.
	// The path looks like: /r/subreddit/comments/postid/slug/
	path := u.Path
	// Remove trailing slash.
	path = strings.TrimRight(path, "/")
	// Remove query and fragment.
	u.RawQuery = ""
	u.Fragment = ""
	// Append .json.
	u.Path = path + ".json"

	return u.String(), nil
}

// parseRedditJSON extracts the selftext from a Reddit .json response body.
// Returns (text, author, permalink).
func parseRedditJSON(body []byte) (string, string, string, error) {
	var posts []redditPost
	if err := json.Unmarshal(body, &posts); err != nil {
		return "", "", "", fmt.Errorf("parse reddit json: %w", err)
	}
	if len(posts) == 0 || len(posts[0].Data.Children) == 0 {
		return "", "", "", fmt.Errorf("reddit json: no post data found")
	}

	post := posts[0].Data.Children[0].Data

	// Prefer selftext (plain text), fall back to stripping HTML from selftext_html.
	text := strings.TrimSpace(post.Selftext)
	if text == "" && post.SelftextHTML != "" {
		text = stripHTML(post.SelftextHTML)
	}
	if text == "" {
		return "", "", "", fmt.Errorf("reddit json: post has no selftext")
	}

	return text, post.Author, post.Permalink, nil
}

// stripHTML is a very basic HTML tag stripper — sufficient for Reddit's
// selftext_html which is usually just <p> and <a> tags.
func stripHTML(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Replace common HTML entities with their unicode equivalents.
	s := b.String()
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	return strings.TrimSpace(s)
}
