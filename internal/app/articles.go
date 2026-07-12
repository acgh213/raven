package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"raven/internal/model"
)

// ArticleService is the persistence boundary for article queries.
type ArticleService interface {
	ListArticles(ctx context.Context, params model.ArticleListParams) (model.ArticleListResult, error)
	GetArticle(ctx context.Context, id string) (model.ArticleDetail, error)
}

type articleListEnvelope struct {
	Articles   []model.ArticleSummary `json:"articles"`
	NextCursor string                 `json:"next_cursor"`
}

func handleListArticles(svc ArticleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeAPIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "list articles requires GET", false)
			return
		}

		params := model.ArticleListParams{
			FeedID: r.URL.Query().Get("feed_id"),
			Cursor: r.URL.Query().Get("cursor"),
			Limit:  20,
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
				params.Limit = n
			}
		}

		// Validate cursor is valid base64 before passing downstream.
		if params.Cursor != "" {
			if _, err := base64.RawURLEncoding.DecodeString(params.Cursor); err != nil {
				writeAPIError(w, r, http.StatusBadRequest, "invalid_cursor", "cursor is not valid base64", false)
				return
			}
		}

		result, err := svc.ListArticles(r.Context(), params)
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "list_articles", "could not list articles", true)
			return
		}

		articles := result.Articles
		if articles == nil {
			articles = []model.ArticleSummary{}
		}

		writeJSON(w, http.StatusOK, articleListEnvelope{
			Articles:   articles,
			NextCursor: result.NextCursor,
		})
	}
}

func handleGetArticle(svc ArticleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeAPIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "get article requires GET", false)
			return
		}

		// Path: /v1/articles/{id}
		id := strings.TrimPrefix(r.URL.Path, "/")
		if id == "" {
			writeAPIError(w, r, http.StatusBadRequest, "missing_id", "article ID is required", false)
			return
		}

		detail, err := svc.GetArticle(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeAPIError(w, r, http.StatusNotFound, "not_found", "article not found", false)
				return
			}
			writeAPIError(w, r, http.StatusInternalServerError, "get_article", "could not get article", true)
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}
