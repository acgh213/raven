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
}

func TestExtractStripsScriptAndStyle(t *testing.T) {
	html := `<html><body>
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

func TestExtractCollapsesWhitespace(t *testing.T) {
	html := `<html><body>
  <p>   Lots   of    spaces   </p>
  <p>

  Blank lines

  </p>
</body></html>`

	result, err := Extract([]byte(html))
	if err != nil {
		t.Fatalf("Extract(): %v", err)
	}
	if strings.Contains(result.Text, "   ") {
		t.Error("multiple spaces not collapsed")
	}
	if strings.Contains(result.Text, "\n\n\n") {
		t.Error("multiple newlines not collapsed")
	}
}

func TestExtractFindsLeadImage(t *testing.T) {
	html := `<html><body>
  <p>Some text.</p>
  <img src="https://example.com/hero.jpg" alt="Hero">
  <p>More text.</p>
  <img src="https://example.com/second.png">
</body></html>`

	result, err := Extract([]byte(html))
	if err != nil {
		t.Fatalf("Extract(): %v", err)
	}
	if result.LeadImageURL != "https://example.com/hero.jpg" {
		t.Errorf("lead image = %q, want https://example.com/hero.jpg", result.LeadImageURL)
	}
}

func TestExtractSkipsDataURIImagesForLead(t *testing.T) {
	html := `<html><body>
  <img src="data:image/png;base64,AAAA">
  <img src="https://example.com/real.jpg">
</body></html>`

	result, err := Extract([]byte(html))
	if err != nil {
		t.Fatalf("Extract(): %v", err)
	}
	if result.LeadImageURL != "https://example.com/real.jpg" {
		t.Errorf("lead image = %q, want https://example.com/real.jpg", result.LeadImageURL)
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
	if result.WordCount != 0 {
		t.Errorf("word count = %d, want 0", result.WordCount)
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
