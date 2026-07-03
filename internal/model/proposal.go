package model

// RepairProposal describes a single candidate edit to a YAML source that the
// syntax repair engine believes will move the document closer to a parseable
// state. Proposals are line-oriented and operate on raw text so that comments,
// ordering, and formatting are preserved.
//
// A proposal is self-contained: applying After in place of Before across the
// [StartLine, EndLine] range (1-based, inclusive) yields the repaired buffer.
type RepairProposal struct {
	RuleID      string           `json:"ruleId"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	SourceFile  string           `json:"sourceFile"`
	StartLine   int              `json:"startLine"`
	EndLine     int              `json:"endLine"`
	Before      string           `json:"before"`
	After       string           `json:"after"`
	Confidence  RepairConfidence `json:"confidence"`
}

// Finding converts a proposal into a Finding so syntax issues can be reported
// alongside schema and policy findings by the existing reporter. Issues that
// break parsing are errors; cosmetic likely-mistakes (a key missing the space
// after its colon, which is still valid YAML) are warnings.
func (p RepairProposal) Finding() Finding {
	sev := SeverityError
	if p.RuleID == "syntax-colon-space" {
		sev = SeverityWarning
	}
	return Finding{
		RuleID:           p.RuleID,
		Message:          p.Description,
		Source:           SourceSyntax,
		Category:         CategorySyntax,
		Severity:         sev,
		RepairConfidence: p.Confidence,
		SourceFile:       p.SourceFile,
		Line:             p.StartLine,
		EndLine:          p.EndLine,
		Repairable:       true,
	}
}
