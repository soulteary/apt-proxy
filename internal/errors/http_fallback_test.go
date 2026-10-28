package errors

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHTTPErrorDefaultsPreserveStructuredCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    *AppError
		status int
		code   Code
	}{{"nil", nil, 500, ErrInternal}, {"derived", &AppError{Code: ErrRateLimited, Message: "slow down"}, 429, ErrRateLimited}, {"unknown", &AppError{Code: Code("CUSTOM"), Message: "custom"}, 500, Code("CUSTOM")}} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteHTTPError(w, tc.err)
			if w.Code != tc.status || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("response: %d %v", w.Code, w.Header())
			}
			var body HTTPErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != tc.code {
				t.Fatalf("code %q", body.Code)
			}
			if tc.err != nil && body.Error != tc.err.Message {
				t.Fatal("message lost")
			}
		})
	}
}
