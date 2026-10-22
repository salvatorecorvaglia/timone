package model

import "testing"

func TestDiagnosticPolicyBounds(t *testing.T) {
	for _, p := range []DiagnosticPolicy{DefaultDiagnosticPolicy(), {0, 60, 1}, {3600, 86400, 10000}} {
		if err := p.Validate(); err != nil {
			t.Fatal(p, err)
		}
	}
	for _, p := range []DiagnosticPolicy{{}, {-1, 900, 5}, {3601, 900, 5}, {300, 59, 5}, {300, 86401, 5}, {300, 900, 0}, {300, 900, 10001}} {
		if err := p.Validate(); err == nil {
			t.Fatal("invalid policy accepted", p)
		}
	}
}
