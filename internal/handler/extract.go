package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"raven/internal/extractor"
	"raven/internal/model"
)

// extractPayload is the JSON payload for fetch_article jobs.
type extractPayload struct {
	ArticleID        string `json:"article_id"`
	ContentVersionID string `json:"content_version_id"`
	ArticleURL       string `json:"article_url"`
}

// Fetcher abstracts HTTP retrieval.
type Fetcher interface {
	Fetch(url string) (*http.Response, error)
}

// ExtractHandler processes fetch_article jobs by fetching an article URL,
// extracting text, and updating the content version.
type ExtractHandler struct {
	fetcher  Fetcher
	articles ArticleStore
}

// ArticleStore is the subset of store.ArticleStore needed by the extract handler.
type ArticleStore interface {
	UpdateContentVersion(
		ctx context.Context,
		versionID string,
		rawHTML []byte,
		extractedText string,
		wordCount int,
		leadImageURL string,
		contentHash string,
		engine string,
		version string,
		status string,
	) error
}

// NewExtractHandler creates an ExtractHandler.
func NewExtractHandler(fetcher Fetcher, articles ArticleStore) *ExtractHandler {
	return &ExtractHandler{
		fetcher:  fetcher,
		articles: articles,
	}
}

// Handle implements jobs.Handler for the fetch_article job kind.
func (h *ExtractHandler) Handle(ctx context.Context, job *model.Job) error {
	var payload extractPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return fmt.Errorf("parse fetch_article payload: %w", err)
	}

	if payload.ContentVersionID == "" || payload.ArticleURL == "" {
		return fmt.Errorf("fetch_article payload missing content_version_id or article_url")
	}

	// Reddit posts: use the .json API to get selftext directly.
	if isRedditURL(payload.ArticleURL) {
		return h.handleReddit(ctx, payload)
	}

	resp, err := h.fetcher.Fetch(payload.ArticleURL)
	if err != nil {
		_ = h.articles.UpdateContentVersion(ctx, payload.ContentVersionID, nil, "", 0, "", "", "", "", "failed")
		return fmt.Errorf("fetch %q: %w", payload.ArticleURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = h.articles.UpdateContentVersion(ctx, payload.ContentVersionID, nil, "", 0, "", "", "", "", "failed")
		return fmt.Errorf("read body for %q: %w", payload.ArticleURL, err)
	}

	result, err := extractor.Extract(body)
	if err != nil {
		_ = h.articles.UpdateContentVersion(ctx, payload.ContentVersionID, nil, "", 0, "", "", "", "", "failed")
		return fmt.Errorf("extract %q: %w", payload.ArticleURL, err)
	}

	hash := sha256.Sum256(body)
	contentHash := hex.EncodeToString(hash[:])

	// Guard against CAPTCHA / bot-wall pages replacing legitimate RSS summaries.
	// If the extractor produced very little text and it smells like a bot wall,
	// mark as completed but keep whatever text currently exists.
	if result.WordCount < 20 && looksLikeBotWall(result.Text) {
		_ = h.articles.UpdateContentVersion(
			ctx, payload.ContentVersionID,
			body, "", result.WordCount,
			result.LeadImageURL, contentHash,
			"raven-extract", "0.1.0", "completed",
		)
		return nil
	}

	if err := h.articles.UpdateContentVersion(
		ctx, payload.ContentVersionID,
		body, result.Text, result.WordCount,
		result.LeadImageURL, contentHash,
		"raven-extract", "0.1.0", "completed",
	); err != nil {
		return fmt.Errorf("update content version: %w", err)
	}

	return nil
}

// looksLikeBotWall returns true if the extracted text appears to be a
// CAPTCHA, bot-detection, or access-denial page rather than real content.
func looksLikeBotWall(text string) bool {
	markers := []string{
		"captcha", "CAPTCHA",
		"not a bot", "not for bots",
		"are you a human", "are you a real person",
		"verify you are human",
		"security check", "security verification",
		"prove you are human",
		"enable javascript",
		"please enable cookies",
		"access denied", "403 forbidden",
		"bot detection", "bot protection",
	}
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// handleReddit fetches a Reddit post via the .json API and extracts selftext.
func (h *ExtractHandler) handleReddit(ctx context.Context, payload extractPayload) error {
	jsonURL, err := redditJSONURL(payload.ArticleURL)
	if err != nil {
		return fmt.Errorf("reddit json url: %w", err)
	}

	resp, err := h.fetcher.Fetch(jsonURL)
	if err != nil {
		_ = h.articles.UpdateContentVersion(ctx, payload.ContentVersionID, nil, "", 0, "", "", "", "", "failed")
		return fmt.Errorf("fetch reddit json %q: %w", jsonURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = h.articles.UpdateContentVersion(ctx, payload.ContentVersionID, nil, "", 0, "", "", "", "", "failed")
		return fmt.Errorf("read reddit json body: %w", err)
	}

	selftext, _, _, err := parseRedditJSON(body)
	if err != nil {
		_ = h.articles.UpdateContentVersion(ctx, payload.ContentVersionID, nil, "", 0, "", "", "", "", "failed")
		return fmt.Errorf("parse reddit json: %w", err)
	}

	// Run selftext through go-readability in case it contains HTML.
	result, err := extractor.Extract([]byte(selftext))
	engine := "reddit-json"
	if err != nil {
		// Fall back to raw selftext if readability chokes.
		result = extractor.Result{
			Text:      selftext,
			WordCount: countWords(selftext),
		}
		engine = "reddit-json-raw"
	}

	hash := sha256.Sum256(body)
	contentHash := hex.EncodeToString(hash[:])

	if err := h.articles.UpdateContentVersion(
		ctx, payload.ContentVersionID,
		body, result.Text, result.WordCount,
		result.LeadImageURL, contentHash,
		engine, "0.1.0", "completed",
	); err != nil {
		return fmt.Errorf("update reddit content version: %w", err)
	}

	return nil
}

// countWords is a simple whitespace-based word counter.
func countWords(text string) int {
	return len(strings.Fields(text))
}
