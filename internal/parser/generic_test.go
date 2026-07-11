package parser

import (
	"errors"
	"testing"
)

func TestValidateYAML(t *testing.T) {
	cases := []struct {
		name  string
		input string
		valid bool
	}{
		{"mapping", "a: 1\nb: 2\n", true},
		{"sequence", "- one\n- two\n", true},
		{"scalar", "just a string\n", true},
		{"multidoc", "a: 1\n---\nb: 2\n", true},
		{"tab indent", "a:\n\tb: 1\n", false},
		{"bad indent", "foo:\n  bar: 1\n baz: 2\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateYAML([]byte(c.input))
			if c.valid && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !c.valid && err == nil {
				t.Fatal("expected invalid, got nil error")
			}
		})
	}
}

func TestParseErrorLine(t *testing.T) {
	err := ValidateYAML([]byte("a:\n\tb: 1\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	line, ok := ParseErrorLine(err)
	if !ok {
		t.Fatalf("expected a line number from %v", err)
	}
	if line < 1 {
		t.Fatalf("line = %d, want >= 1", line)
	}
}

func TestParseErrorLineNoMatch(t *testing.T) {
	if _, ok := ParseErrorLine(errors.New("boom")); ok {
		t.Fatal("expected no line number")
	}
}
