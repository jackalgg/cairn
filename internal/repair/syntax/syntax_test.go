package syntax

import (
	"strings"
	"testing"

	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/parser"
)

func TestDetectTabs(t *testing.T) {
	input := []byte("spec:\n\tcontainers:\n\t- name: app\n")
	proposals := Detect("test.yaml", input)
	if len(proposals) == 0 {
		t.Fatal("expected a tab proposal")
	}
	if proposals[0].RuleID != "syntax-tabs" {
		t.Fatalf("RuleID = %q, want syntax-tabs", proposals[0].RuleID)
	}
	if proposals[0].Confidence != model.RepairCertain {
		t.Fatalf("tab fix should be certain, got %q", proposals[0].Confidence)
	}
	fixed := Apply(input, proposals[0])
	if strings.Contains(string(fixed), "\t") {
		t.Fatalf("tabs not removed: %q", fixed)
	}
}

func TestDetectColonSpacing(t *testing.T) {
	input := []byte("server:\n  host:localhost\n  port:8080\n")
	proposals := Detect("test.yaml", input)
	var colon int
	for _, p := range proposals {
		if p.RuleID == "syntax-colon-space" {
			colon++
		}
	}
	if colon != 2 {
		t.Fatalf("expected 2 colon-space proposals, got %d", colon)
	}
}

func TestColonSpacingSkipsURL(t *testing.T) {
	input := []byte("url: http://example.com\n")
	for _, p := range colonProposals("test.yaml", input) {
		t.Fatalf("did not expect a proposal for a URL value, got %+v", p)
	}
}

func TestRepairAutoFixesIndentation(t *testing.T) {
	input := []byte("foo:\n  bar: 1\n baz: 2\n")
	if parser.ValidateYAML(input) == nil {
		t.Fatal("test input should be invalid YAML")
	}
	res := RepairAuto("test.yaml", input, model.RepairHeuristic)
	if !res.Changed {
		t.Fatal("expected RepairAuto to change the input")
	}
	if err := parser.ValidateYAML(res.Data); err != nil {
		t.Fatalf("repaired YAML still invalid: %v\n%s", err, res.Data)
	}
}

func TestRepairAutoCertainOnlyLeavesHeuristics(t *testing.T) {
	// Colon spacing is heuristic; certain-only auto repair must not touch it.
	input := []byte("server:\n  host:localhost\n")
	res := RepairAuto("test.yaml", input, model.RepairCertain)
	if res.Changed {
		t.Fatalf("certain-only repair should not apply heuristic fixes: %s", res.Data)
	}
}

func TestRepairAutoExpandsTabsAndParses(t *testing.T) {
	input := []byte("metadata:\n\tname: x\n")
	res := RepairAuto("test.yaml", input, model.RepairCertain)
	if !res.Changed {
		t.Fatal("expected tab expansion")
	}
	if err := parser.ValidateYAML(res.Data); err != nil {
		t.Fatalf("expected valid YAML after tab expansion: %v", err)
	}
}

func TestRepairAutoAddsMissingListMarker(t *testing.T) {
	input := []byte("items:\n  - a\n  b\n")
	if parser.ValidateYAML(input) == nil {
		t.Fatal("test input should be invalid YAML")
	}
	res := RepairAuto("test.yaml", input, model.RepairHeuristic)
	if err := parser.ValidateYAML(res.Data); err != nil {
		t.Fatalf("repaired YAML still invalid: %v\n%s", err, res.Data)
	}
	if !strings.Contains(string(res.Data), "- b") {
		t.Fatalf("expected a list marker to be added, got:\n%s", res.Data)
	}
}

func TestBlockReflowFixesCascadingIndent(t *testing.T) {
	// Two wrong lines whose errors are interdependent — no single-line fix
	// makes progress, so blockReflowProposal must fire.
	input := []byte("spec:\nreplicas: 1\n  selector:\n      matchLabels:\n        app: foo\n   template:\n  metadata:\n    labels:\n      app: foo\n")
	if parser.ValidateYAML(input) == nil {
		t.Fatal("test input should be invalid YAML")
	}
	res := RepairAuto("test.yaml", input, model.RepairHeuristic)
	if err := parser.ValidateYAML(res.Data); err != nil {
		t.Fatalf("repaired YAML still invalid: %v\n%s", err, res.Data)
	}
}

func TestBlockReflowFixesMultiLevelMisindent(t *testing.T) {
	// Simulates a block where several lines are indented by the wrong base
	// amount, causing a parse failure that no single-line fix can resolve.
	input := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cfg\ndata:\n  key: value\n      nested:\n        deep: item\n      other: val\n")
	if parser.ValidateYAML(input) == nil {
		t.Fatal("test input should be invalid YAML")
	}
	res := RepairAuto("test.yaml", input, model.RepairHeuristic)
	if err := parser.ValidateYAML(res.Data); err != nil {
		t.Fatalf("repaired YAML still invalid: %v\n%s", err, res.Data)
	}
}

func TestBlockReflowPreservesAlreadyValidContent(t *testing.T) {
	// Valid YAML must never be modified by the block reflow path.
	input := []byte("spec:\n  replicas: 1\n  selector:\n    matchLabels:\n      app: foo\n")
	res := RepairAuto("test.yaml", input, model.RepairHeuristic)
	if res.Changed {
		t.Fatalf("block reflow must not modify valid YAML:\n%s", res.Data)
	}
}

func TestApplyOutOfRangeIsNoop(t *testing.T) {
	input := []byte("a: 1\n")
	p := model.RepairProposal{StartLine: 5, EndLine: 6, After: "x"}
	if got := Apply(input, p); string(got) != string(input) {
		t.Fatalf("expected no-op, got %q", got)
	}
}
