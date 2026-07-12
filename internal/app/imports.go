package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"raven/internal/feed"
	"raven/internal/model"
	"raven/internal/store"
)

const maxOPMLImportBytes = 2 << 20

type requestIDContextKey struct{}

// FeedImportService is the persistence boundary for OPML preview and import.
type FeedImportService interface {
	PreviewImport(context.Context, []model.FeedCandidate) (model.FeedImportPreview, error)
	Import(context.Context, []model.FeedCandidate) (model.FeedImportResult, error)
	ImportIdempotently(context.Context, string, string, []model.FeedCandidate) (model.FeedImportResult, bool, error)
}

// Config wires transport dependencies without coupling app to a concrete store.
type Config struct {
	APIToken    string
	FeedImports FeedImportService
}

type feedImportResponse struct {
	New        []model.FeedCandidate          `json:"new"`
	Created    []model.Feed                   `json:"created"`
	Duplicates []model.FeedCandidate          `json:"duplicates"`
	Invalid    []model.FeedCandidateRejection `json:"invalid"`
}

func newFeedImportResponse() feedImportResponse {
	return feedImportResponse{
		New:        []model.FeedCandidate{},
		Created:    []model.Feed{},
		Duplicates: []model.FeedCandidate{},
		Invalid:    []model.FeedCandidateRejection{},
	}
}

func nonNilSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

type apiErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"request_id"`
}

func withBearerToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			writeAPIError(w, r, http.StatusServiceUnavailable, "api_not_configured", "API bearer token is not configured", false)
			return
		}
		provided := r.Header.Get("Authorization")
		expected := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="raven"`)
			writeAPIError(w, r, http.StatusUnauthorized, "unauthorized", "valid bearer token required", false)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleFeedImport(imports FeedImportService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeAPIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "feed import requires POST", false)
			return
		}
		if !isOPMLContentType(r.Header.Get("Content-Type")) {
			writeAPIError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "OPML import requires an XML content type", false)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxOPMLImportBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeAPIError(w, r, http.StatusRequestEntityTooLarge, "opml_too_large", "OPML import exceeds the 2 MiB limit", false)
				return
			}
			writeAPIError(w, r, http.StatusBadRequest, "read_import", "could not read OPML import", true)
			return
		}

		candidates, err := feed.ParseOPML(body)
		if err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "invalid_opml", err.Error(), false)
			return
		}
		valid, invalid := feed.ValidateCandidates(candidates)
		if r.URL.Query().Get("dry_run") == "true" {
			preview, err := imports.PreviewImport(r.Context(), valid)
			if err != nil {
				writeAPIError(w, r, http.StatusInternalServerError, "preview_import", "could not preview OPML import", true)
				return
			}
			response := newFeedImportResponse()
			response.New = nonNilSlice(preview.New)
			response.Duplicates = nonNilSlice(preview.Duplicates)
			response.Invalid = nonNilSlice(invalid)
			writeJSON(w, http.StatusOK, response)
			return
		}

		var result model.FeedImportResult
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			result, _, err = imports.ImportIdempotently(r.Context(), key, hashImportBody(body), valid)
			if errors.Is(err, store.ErrIdempotencyKeyConflict) {
				writeAPIError(w, r, http.StatusConflict, "idempotency_key_conflict", "idempotency key was already used for a different import", false)
				return
			}
		} else {
			result, err = imports.Import(r.Context(), valid)
		}
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "import_feeds", "could not import OPML feeds", true)
			return
		}
		response := newFeedImportResponse()
		response.Created = nonNilSlice(result.Created)
		response.Duplicates = nonNilSlice(result.Duplicates)
		response.Invalid = nonNilSlice(invalid)
		writeJSON(w, http.StatusOK, response)
	}
}

func isOPMLContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "application/xml", "text/xml", "application/opml+xml":
		return true
	default:
		return false
	}
}

func hashImportBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool) {
	requestID, _ := r.Context().Value(requestIDContextKey{}).(string)
	writeJSON(w, status, apiErrorResponse{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		RequestID: requestID,
	})
}
