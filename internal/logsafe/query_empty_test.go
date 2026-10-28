package logsafe

import "testing"

func TestEmptyQueryHasNoRedactionMarker(t *testing.T) {
	if got := Query(""); got != "" {
		t.Fatalf("empty query became %q", got)
	}
}
