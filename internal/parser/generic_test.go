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

func TestParseAcceptsNonKubernetesYAML(t *testing.T) {
	docs, err := Parse("config.yaml", []byte("name: pipeline\nsteps:\n  - build\n  - test\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if docs[0].IsKubernetes() {
		t.Fatal("plain config should not be detected as Kubernetes")
	}
}

func TestParseDetectsKubernetesYAML(t *testing.T) {
	docs, err := Parse("pod.yaml", []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: p\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != 1 || !docs[0].IsKubernetes() {
		t.Fatalf("expected one Kubernetes document, got %+v", docs)
	}
}

func TestParseTopLevelSequence(t *testing.T) {
	docs, err := Parse("list.yaml", []byte("- a\n- b\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != 1 || docs[0].IsKubernetes() {
		t.Fatalf("top-level sequence should parse as a non-Kubernetes doc, got %+v", docs)
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
