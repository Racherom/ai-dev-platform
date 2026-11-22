package dbsetup

import "testing"

func TestQuoteLiteral(t *testing.T) {
	for in, want := range map[string]string{
		"plain":         "'plain'",
		"it's":          "'it''s'",
		`back\slash`:    `'back\slash'`,
		"'; DROP x; --": "'''; DROP x; --'",
	} {
		if got := quoteLiteral(in); got != want {
			t.Errorf("quoteLiteral(%q) = %s, want %s", in, got, want)
		}
	}
}
