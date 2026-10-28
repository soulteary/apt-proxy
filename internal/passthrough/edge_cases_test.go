package passthrough

import "testing"

func TestNilRulesAndMalformedAuthorityFailClosed(t *testing.T) {
	var empty *List
	if empty.Rules() != nil {
		t.Fatal("nil list produced rules")
	}
	list, err := Parse([]string{"repo.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := list.Match(":80"); ok {
		t.Fatal("empty host matched")
	}
	if _, err := Parse([]string{"repo.example.org%zz"}); err == nil {
		t.Fatal("escaped malformed authority accepted")
	}
}
func TestPublicLiteralOriginMatchesOnlyItsHost(t *testing.T) {
	list, err := Parse([]string{"https://8.8.8.8"})
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := list.Match("8.8.8.8:443")
	if !ok || !rule.ForceHTTPS {
		t.Fatal("public literal not matched")
	}
	if _, ok := list.Match("8.8.4.4"); ok {
		t.Fatal("different address matched")
	}
}
