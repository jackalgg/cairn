package k8serrors

import (
	"path/filepath"
	"testing"

	"github.com/jackalgg/cairn/internal/parser"
)

func TestParseErrorText_runAsNonRoot(t *testing.T) {
	text := `Error from server: pods "app" is forbidden: must runAsNonRoot`
	ids := ParseErrorText(text)
	if len(ids) != 1 || ids[0] != "pss-run-as-non-root" {
		t.Fatalf("ParseErrorText = %v, want [pss-run-as-non-root]", ids)
	}
}

func TestParseErrorText_readOnlyRootFS(t *testing.T) {
	text := `container has readOnlyRootFilesystem set to false`
	ids := ParseErrorText(text)
	if len(ids) != 1 || ids[0] != "pss-read-only-rootfs" {
		t.Fatalf("ParseErrorText = %v, want [pss-read-only-rootfs]", ids)
	}
}

func TestFindingsFromError(t *testing.T) {
	docs, err := parser.ParseFile(filepath.Join("..", "..", "test.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	text := `must runAsNonRoot`
	findings := FindingsFromError(docs[0], text, "1.30")
	if len(findings) == 0 {
		t.Fatal("expected findings from error text")
	}
	for _, f := range findings {
		if f.Source != "kubectl" {
			t.Errorf("source = %q, want kubectl", f.Source)
		}
		if f.GVKString == "" {
			t.Error("GVKString should be set")
		}
	}
}
