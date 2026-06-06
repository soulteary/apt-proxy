package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	logger "github.com/soulteary/logger-kit"
)

func newTestMirrorsHandler(reload func()) *MirrorsHandler {
	return NewMirrorsHandler(logger.New(logger.Config{Format: logger.FormatJSON, Level: logger.ErrorLevel}), reload)
}

func TestMirrorsHandlerRefreshCallsReloadFunc(t *testing.T) {
	called := 0
	h := newTestMirrorsHandler(func() { called++ })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mirrors/refresh", nil)
	h.HandleMirrorsRefresh(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if called != 1 {
		t.Errorf("expected reloadFunc to be called once, got %d", called)
	}

	var got MirrorsRefreshResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Success || got.Message == "" {
		t.Errorf("unexpected response: %+v", got)
	}
}

func TestMirrorsHandlerRefreshRejectsGet(t *testing.T) {
	h := newTestMirrorsHandler(nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mirrors/refresh", nil)
	h.HandleMirrorsRefresh(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
