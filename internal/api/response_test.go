package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestCalculateHitRate(t *testing.T) {
	tests := []struct {
		hits, misses int64
		want         float64
	}{
		{0, 0, 0},
		{10, 0, 1.0},
		{0, 10, 0},
		{75, 25, 0.75},
		{1, 1, 0.5},
	}
	for _, tt := range tests {
		got := CalculateHitRate(tt.hits, tt.misses)
		if got != tt.want {
			t.Errorf("CalculateHitRate(%d,%d) = %v, want %v", tt.hits, tt.misses, got, tt.want)
		}
	}
}

func TestFormatBytesAPI(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{-1, "0 B"},
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.00 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
	}
	for _, tt := range tests {
		got := FormatBytes(tt.in)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]any{"answer": 42, "ok": true}
	if err := WriteJSON(rec, 201, payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q, want application/json", got)
	}
	if rec.Code != 201 {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	var decoded map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["ok"] != true {
		t.Errorf("ok field missing/wrong: %v", decoded)
	}
}
