// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

func TestMirrorsHandlerRefreshErrorsWithoutReloadFunc(t *testing.T) {
	h := newTestMirrorsHandler(nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mirrors/refresh", nil)
	h.HandleMirrorsRefresh(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
