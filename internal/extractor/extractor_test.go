package extractor

import (
	"strings"
	"testing"
)

func TestExtractPlainTextFromBasicHTML(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
  <article>
    <h1>Hello World</h1>
    <p>This is a paragraph with <strong>bold</strong> text.</p>
    <p>Another paragraph here.</p>
  </article>
</body>
</html>`

	result, err := Extract([]byte(html))
	if err != nil {
		t.Fatalf("Extract(): %v", err)
	}
	if result.Text == "" {
		t.Fatal("extracted text is empty")
	}
	if !strings.Contains(result.Text, "Hello World") {
		t.Errorf("text does not contain heading: %q", result.Text)
	}
	if !strings.Contains(result.Text, "bold text") {
		t.Errorf("text does not contain bold text: %q", result.Text)
	}
	if result.WordCount == 0 {
		t.Errorf("word count is 0")
	}
	// go-readability extracts the title too.
	if result.Title != "Test Page" {
		t.Errorf("title = %q, want 'Test Page'", result.Title)
	}
}

func TestExtractStripsScriptAndStyle(t *testing.T) {
	html := `<html><head><title>Clean</title></head><body>
  <script>console.log("hidden");</script>
  <style>.hidden { display: none; }</style>
  <p>Visible text.</p>
</body></html>`

	result, err := Extract([]byte(html))
	if err != nil {
		t.Fatalf("Extract(): %v", err)
	}
	if strings.Contains(result.Text, "console.log") {
		t.Error("script content leaked into extracted text")
	}
	if strings.Contains(result.Text, "display: none") {
		t.Error("style content leaked into extracted text")
	}
	if !strings.Contains(result.Text, "Visible text") {
		t.Error("visible text not extracted")
	}
}

func TestExtractFindsLeadImage(t *testing.T) {
	// go-readability extracts the lead image from og:image meta tags.
	html := `<!DOCTYPE html>
<html><head>
  <title>Images</title>
  <meta property="og:image" content="https://example.com/hero.jpg">
</head>
<body>
  <p>Substantial article text here that gives the readability algorithm enough content to work with and recognize this as a genuine article worthy of extraction.</p>
</body></html>`

	result, err := Extract([]byte(html))
	if err != nil {
		t.Fatalf("Extract(): %v", err)
	}
	if result.LeadImageURL != "https://example.com/hero.jpg" {
		t.Errorf("lead image = %q, want https://example.com/hero.jpg", result.LeadImageURL)
	}
}

func TestExtractHandlesEmptyInput(t *testing.T) {
	result, err := Extract([]byte{})
	if err != nil {
		t.Fatalf("Extract() on empty input: %v", err)
	}
	if result.Text != "" {
		t.Errorf("text = %q, want empty", result.Text)
	}
}

func TestExtractHandlesTextOnlyInput(t *testing.T) {
	result, err := Extract([]byte("Just plain text, no HTML tags at all."))
	if err != nil {
		t.Fatalf("Extract() on text-only: %v", err)
	}
	if !strings.Contains(result.Text, "plain text") {
		t.Errorf("text = %q, want to contain 'plain text'", result.Text)
	}
	if result.WordCount < 2 {
		t.Errorf("word count = %d, want >= 2", result.WordCount)
	}
}

func TestExtractHandlesBoilerplateRemoval(t *testing.T) {
	// Simulate a page with nav, sidebar, footer — readability should strip them.
	html := `<!DOCTYPE html>
<html><head><title>Article</title></head>
<body>
  <nav>Home About Contact</nav>
  <main>
    <article>
      <h1>The Real Content</h1>
      <p>This is the actual article body that should be extracted.</p>
      <p>It spans multiple paragraphs.</p>
    </article>
  </main>
  <aside>Related links and ads</aside>
  <footer>Copyright 2026</footer>
</body></html>`

	result, err := Extract([]byte(html))
	if err != nil {
		t.Fatalf("Extract(): %v", err)
	}
	if !strings.Contains(result.Text, "actual article body") {
		t.Errorf("main content not extracted: %q", result.Text)
	}
	// Boilerplate should not appear.
	if strings.Contains(result.Text, "Copyright") {
		t.Errorf("footer boilerplate leaked into extracted text: %q", result.Text)
	}
}
