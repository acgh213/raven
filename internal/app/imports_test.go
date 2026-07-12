package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"raven/internal/db"
	"raven/internal/model"
	"raven/internal/store"
)

const importTestToken = "raven-test-token"

func newFeedImportHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	return New(Config{
		APIToken:    importTestToken,
		FeedImports: store.NewFeedStore(database, model.RealClock{}),
	}), database
}

func importRequest(t *testing.T, url, body string, withToken bool) *httptest.ResponseRecorder {
	t.Helper()
	handler, _ := newFeedImportHandler(t)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/xml")
	if withToken {
		req.Header.Set("Authorization", "Bearer "+importTestToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestFeedImportRequiresBearerToken(t *testing.T) {
	response := importRequest(t, "/v1/feeds/import", `<opml><body><outline xmlUrl="https://example.com/feed.xml"/></body></opml>`, false)

	if response.Code != http.StatusUnauthorized {
		t.Errorf("POST /v1/feeds/import without token = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Error("unauthorized import response lacks X-Request-ID")
	}
}

func TestFeedImportWrongMethodUsesJSONContract(t *testing.T) {
	handler, _ := newFeedImportHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/import", nil)
	req.Header.Set("Authorization", "Bearer "+importTestToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v1/feeds/import = %d, want %d; body=%s", response.Code, http.StatusMethodNotAllowed, response.Body.String())
	}
	if response.Header().Get("Allow") != http.MethodPost {
		t.Errorf("Allow = %q, want POST", response.Header().Get("Allow"))
	}
	var body apiErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode method error: %v", err)
	}
	if body.Code != "method_not_allowed" || body.RequestID == "" {
		t.Errorf("method error = %+v, want stable code and request ID", body)
	}
}

func TestFeedImportPreviewCanonicalizesAndDoesNotPersist(t *testing.T) {
	body := `<opml><body>
  <outline xmlUrl="HTTPS://EXAMPLE.COM:443/feed#fragment" text="Valid"/>
  <outline xmlUrl="ftp://example.com/not-allowed" text="Invalid"/>
</body></opml>`
	response := importRequest(t, "/v1/feeds/import?dry_run=true", body, true)

	if response.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var got feedImportResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if len(got.New) != 1 || got.New[0].URL != "https://example.com/feed" {
		t.Errorf("preview new = %+v, want one canonicalized candidate", got.New)
	}
	if len(got.Invalid) != 1 || got.Invalid[0].URL != "ftp://example.com/not-allowed" {
		t.Errorf("preview invalid = %+v, want one rejected candidate", got.Invalid)
	}
}

func TestFeedImportResponseUsesStableJSONFieldNames(t *testing.T) {
	handler, _ := newFeedImportHandler(t)
	body := `<opml><body><outline xmlUrl="https://example.com/feed.xml" text="Example"/></body></opml>`
	req := httptest.NewRequest(http.MethodPost, "/v1/feeds/import", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("Authorization", "Bearer "+importTestToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	var payload struct {
		Created []map[string]any `json:"created"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if len(payload.Created) != 1 {
		t.Fatalf("created response = %+v, want one feed", payload.Created)
	}
	if _, ok := payload.Created[0]["url"]; !ok {
		t.Errorf("created feed JSON = %+v, want lowercase url field", payload.Created[0])
	}
	if _, ok := payload.Created[0]["URL"]; ok {
		t.Errorf("created feed JSON = %+v, must not expose Go field name URL", payload.Created[0])
	}
}

func TestFeedImportResponseUsesEmptyArraysInsteadOfNull(t *testing.T) {
	response := importRequest(t, "/v1/feeds/import?dry_run=true", `<opml><body></body></opml>`, true)
	if response.Code != http.StatusOK {
		t.Fatalf("empty preview status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}

	var payload map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode empty preview response: %v", err)
	}
	for _, field := range []string{"new", "created", "duplicates", "invalid"} {
		if string(payload[field]) != "[]" {
			t.Errorf("%s = %s, want []", field, payload[field])
		}
	}
}

func TestFeedImportIdempotencyKeyReplaysHTTPResult(t *testing.T) {
	handler, database := newFeedImportHandler(t)
	post := func(body string) (*httptest.ResponseRecorder, feedImportResponse) {
		req := httptest.NewRequest(http.MethodPost, "/v1/feeds/import", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Authorization", "Bearer "+importTestToken)
		req.Header.Set("Idempotency-Key", "retry-key-1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var result feedImportResponse
		if response.Code == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatalf("decode import response: %v", err)
			}
		}
		return response, result
	}

	body := `<opml><body><outline xmlUrl="https://example.com/feed.xml" text="Example"/></body></opml>`
	firstResponse, first := post(body)
	if firstResponse.Code != http.StatusOK || len(first.Created) != 1 {
		t.Fatalf("first keyed import = status %d, result %+v", firstResponse.Code, first)
	}
	secondResponse, second := post(body)
	if secondResponse.Code != http.StatusOK || len(second.Created) != 1 || second.Created[0].ID != first.Created[0].ID {
		t.Errorf("replayed keyed import = status %d, result %+v; want original created result", secondResponse.Code, second)
	}

	conflictResponse, _ := post(`<opml><body><outline xmlUrl="https://example.com/other.xml" text="Other"/></body></opml>`)
	if conflictResponse.Code != http.StatusConflict {
		t.Errorf("changed keyed import = status %d, want %d; body=%s", conflictResponse.Code, http.StatusConflict, conflictResponse.Body.String())
	}

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count); err != nil {
		t.Fatalf("count feeds: %v", err)
	}
	if count != 1 {
		t.Errorf("persisted feed count = %d, want 1", count)
	}
}

func TestFeedImportPersistsAndReportsSecondImportAsDuplicate(t *testing.T) {
	handler, database := newFeedImportHandler(t)
	body := `<opml><body><outline xmlUrl="https://example.com/feed.xml" text="Example"/></body></opml>`

	post := func() feedImportResponse {
		req := httptest.NewRequest(http.MethodPost, "/v1/feeds/import", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Authorization", "Bearer "+importTestToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("import status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
		}
		var got feedImportResponse
		if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
			t.Fatalf("decode import response: %v", err)
		}
		return got
	}

	first := post()
	if len(first.Created) != 1 || len(first.Duplicates) != 0 {
		t.Errorf("first import = %+v, want one created feed", first)
	}
	second := post()
	if len(second.Created) != 0 || len(second.Duplicates) != 1 {
		t.Errorf("second import = %+v, want one duplicate", second)
	}

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count); err != nil {
		t.Fatalf("count feeds: %v", err)
	}
	if count != 1 {
		t.Errorf("persisted feed count = %d, want 1", count)
	}
}
