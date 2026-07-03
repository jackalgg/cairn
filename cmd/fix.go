package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/jackalgg/cairn/internal/cluster"
	"github.com/jackalgg/cairn/internal/engine"
	"github.com/jackalgg/cairn/internal/fixer"
	"github.com/jackalgg/cairn/internal/k8serrors"
	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/parser"
	"github.com/jackalgg/cairn/internal/repair/syntax"
	"github.com/jackalgg/cairn/internal/schema"
	"github.com/jackalgg/cairn/internal/verify"
	"github.com/spf13/cobra"
)

var (
	fixDryRun      bool
	fixOut         string
	fixIDs         []string
	fixFromError   string
	fixStdinErrors bool
	fixInteractive bool
	fixYes         bool
	fixVerify      bool
)

var fixCmd = &cobra.Command{
	Use:   "fix [path]",
	Short: "Repair misconfigured YAML so it parses and runs",
	Long: `Repair the common mistakes that keep YAML from loading: tab indentation,
inconsistent indentation, missing list markers, and keys missing the space after
a colon.

By default fix runs interactively, presenting each proposed repair for you to
accept or skip. High-confidence fixes (tab expansion) are always safe; structural
guesses (indentation, list markers) are shown so you stay in control.

When input is piped on stdin or you pass --yes, fix applies repairs without
prompting. Kubernetes resources additionally receive any available policy and
API-compatibility fixes.`,
	Args: cobra.ExactArgs(1),
	RunE: runFix,
}

func runFix(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	eng, err := newEngine(ctx, schemaValidation, true, true)
	if err != nil {
		return err
	}

	path := args[0]
	sources, err := collectSources(path)
	if err != nil {
		return err
	}

	interactive := resolveInteractive(path)
	stdin := bufio.NewReader(os.Stdin)

	var errorText string
	if fixFromError != "" || fixStdinErrors {
		errorText, err = readErrorText(fixFromError, fixStdinErrors)
		if err != nil {
			return err
		}
	}

	verifier := buildVerifier()
	if fixVerify {
		fmt.Fprintf(os.Stderr, "Verification: %s\n", verifier.Backend())
	}

	changedAny := false
	remainingErrors := false

	for _, src := range sources {
		repaired, applied, quit := repairSyntax(src, interactive, stdin)

		final, err := applyKubernetesFixes(ctx, eng, src.name, repaired, errorText)
		if err != nil {
			return err
		}

		// Verify the repaired manifest would apply, feeding any dry-run errors
		// back into the fixer until it passes or stops making progress.
		if fixVerify && verifier.Available() && parser.ValidateYAML(final) == nil {
			final, err = verifyAndRepair(ctx, eng, verifier, src.name, final)
			if err != nil {
				return err
			}
		}

		if !bytes.Equal(final, src.data) {
			changedAny = true
			if err := emitResult(src.name, src.data, final); err != nil {
				return err
			}
		} else if len(applied) == 0 && parser.ValidateYAML(final) == nil {
			fmt.Fprintf(os.Stderr, "%s: nothing to repair\n", src.name)
		}

		if err := parser.ValidateYAML(final); err != nil {
			remainingErrors = true
			fmt.Fprintf(os.Stderr, "%s: still not valid YAML after repair: %v\n", src.name, err)
			if quit {
				break
			}
			continue
		}

		if fixVerify && verifier.Available() {
			if !reportVerification(ctx, eng, verifier, src.name, final) {
				remainingErrors = true
			}
		}

		if quit {
			break
		}
	}

	if !changedAny && !remainingErrors {
		return nil
	}
	if remainingErrors {
		return fmt.Errorf("unresolved errors remain after repair")
	}
	return nil
}

// buildVerifier picks a verification backend: a live-cluster server-side dry-run
// when a cluster is reachable, otherwise offline schema validation when --schema
// is set. When neither is available verification is unavailable (skipped).
func buildVerifier() *verify.Verifier {
	if client, err := cluster.NewClient(kubeconfig, kubeContext); err == nil {
		return verify.New(verify.NewClusterRunner(client))
	}
	if schemaValidation {
		if v, err := schema.New(schema.FormatSchemaVersion(kubernetesVersion), true); err == nil {
			return verify.New(verify.NewSchemaRunner(v))
		}
	}
	return verify.New(nil)
}

// verifyAndRepair runs the dry-run verify loop: while the manifest is rejected,
// translate the rejection into findings and apply fixes, bounded by rounds.
func verifyAndRepair(ctx context.Context, eng *engine.Engine, verifier *verify.Verifier, source string, data []byte) ([]byte, error) {
	rounds := fixMaxRounds
	if rounds < 1 {
		rounds = 1
	}
	final := data
	for round := 0; round < rounds; round++ {
		results := verifyDocs(ctx, eng, verifier, source, final)
		if verify.AllOK(results) {
			return final, nil
		}
		failText := verify.FailureText(results)
		next, err := applyKubernetesFixes(ctx, eng, source, final, failText)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(next, final) {
			return final, nil // no further progress possible
		}
		final = next
	}
	return final, nil
}

// verifyDocs scans data into documents and verifies each one.
func verifyDocs(ctx context.Context, eng *engine.Engine, verifier *verify.Verifier, source string, data []byte) []verify.Result {
	result, err := eng.ScanBytes(ctx, source, data)
	if err != nil {
		return []verify.Result{{OK: false, Message: err.Error()}}
	}
	return verifier.Verify(ctx, result.Documents)
}

// reportVerification prints per-document verification status and returns whether
// every Kubernetes document would apply.
func reportVerification(ctx context.Context, eng *engine.Engine, verifier *verify.Verifier, source string, data []byte) bool {
	results := verifyDocs(ctx, eng, verifier, source, data)
	if len(results) == 0 {
		return true
	}
	ok := true
	for _, r := range results {
		name := r.Name
		if name == "" {
			name = "(unnamed)"
		}
		if r.OK {
			fmt.Fprintf(os.Stderr, "VERIFIED %s %s (%s)\n", r.GVK, name, verifier.Backend())
		} else {
			ok = false
			fmt.Fprintf(os.Stderr, "VERIFY FAILED %s %s: %s\n", r.GVK, name, r.Message)
		}
	}
	return ok
}

type rawSource struct {
	name string
	data []byte
}

func collectSources(path string) ([]rawSource, error) {
	if path == "-" {
		source, data, err := parser.ReadPath("-")
		if err != nil {
			return nil, err
		}
		return []rawSource{{name: source, data: data}}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []rawSource{{name: path, data: data}}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var sources []rawSource
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		full := filepath.Join(path, entry.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		sources = append(sources, rawSource{name: full, data: data})
	}
	return sources, nil
}

// resolveInteractive decides whether to prompt. Interactive review requires a
// TTY on stdin and a YAML source that is not stdin itself (which would conflict
// with reading answers).
func resolveInteractive(path string) bool {
	if fixYes {
		return false
	}
	if path == "-" {
		return false
	}
	if !fixInteractive {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func repairSyntax(src rawSource, interactive bool, stdin *bufio.Reader) ([]byte, []model.RepairProposal, bool) {
	if !interactive {
		conf := model.RepairCertain
		if fixYes {
			conf = model.RepairHeuristic
		}
		res := syntax.RepairAuto(src.name, src.data, conf)
		return res.Data, res.Applied, false
	}
	return interactiveRepair(src, stdin)
}

func interactiveRepair(src rawSource, stdin *bufio.Reader) ([]byte, []model.RepairProposal, bool) {
	current := append([]byte(nil), src.data...)
	skipped := map[string]bool{}
	var applied []model.RepairProposal
	acceptAll := false

	const cap = 200
	for i := 0; i < cap; i++ {
		proposals := syntax.Detect(src.name, current)
		var next *model.RepairProposal
		for j := range proposals {
			if !skipped[syntax.Signature(proposals[j])] {
				next = &proposals[j]
				break
			}
		}
		if next == nil {
			break
		}

		if acceptAll {
			current = syntax.Apply(current, *next)
			applied = append(applied, *next)
			continue
		}

		printProposal(src.name, *next)
		switch promptChoice(stdin) {
		case "y":
			current = syntax.Apply(current, *next)
			applied = append(applied, *next)
		case "a":
			acceptAll = true
			current = syntax.Apply(current, *next)
			applied = append(applied, *next)
		case "q":
			return current, applied, true
		default: // "n"
			skipped[syntax.Signature(*next)] = true
		}
	}
	return current, applied, false
}

func printProposal(source string, p model.RepairProposal) {
	loc := source
	if p.StartLine > 0 {
		if p.EndLine > p.StartLine {
			loc = fmt.Sprintf("%s:%d-%d", source, p.StartLine, p.EndLine)
		} else {
			loc = fmt.Sprintf("%s:%d", source, p.StartLine)
		}
	}
	fmt.Fprintf(os.Stderr, "\n%s  (%s, %s)\n", p.Title, loc, p.Confidence)
	fmt.Fprintf(os.Stderr, "  %s\n", p.Description)
	for _, line := range strings.Split(p.Before, "\n") {
		fmt.Fprintf(os.Stderr, "  - %s\n", line)
	}
	for _, line := range strings.Split(p.After, "\n") {
		fmt.Fprintf(os.Stderr, "  + %s\n", line)
	}
}

func promptChoice(stdin *bufio.Reader) string {
	fmt.Fprint(os.Stderr, "Apply this repair? [y]es / [n]o / [a]ll / [q]uit: ")
	line, err := stdin.ReadString('\n')
	if err != nil {
		return "q"
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return "y"
	case "a", "all":
		return "a"
	case "q", "quit":
		return "q"
	default:
		return "n"
	}
}

// applyKubernetesFixes runs the existing findings-based fixer on the repaired
// content for Kubernetes resources (policy, schema-structure, API compat). It
// returns the final bytes; non-Kubernetes YAML passes through unchanged.
func applyKubernetesFixes(ctx context.Context, eng *engine.Engine, source string, data []byte, errorText string) ([]byte, error) {
	if parser.ValidateYAML(data) != nil {
		return data, nil
	}
	result, err := eng.ScanBytes(ctx, source, data)
	if err != nil {
		return data, nil
	}

	findings := result.Findings
	if errorText != "" {
		ruleIDs := k8serrors.RuleIDsForFix(errorText)
		if len(ruleIDs) > 0 {
			findings = engine.FilterByRuleID(findings, ruleIDs)
		}
		if len(findings) == 0 {
			for _, doc := range result.Documents {
				findings = append(findings, k8serrors.FindingsFromError(doc, errorText, engTargetVersion(eng))...)
			}
		}
	}
	if len(fixIDs) > 0 {
		findings = engine.FilterByRuleID(findings, fixIDs)
	}
	findings = engine.FilterFixable(findings)
	if len(findings) == 0 {
		return data, nil
	}

	fileResults, err := fixer.Apply(result, findings, fixer.Options{DryRun: true})
	if err != nil {
		return nil, err
	}
	final := data
	for _, fr := range fileResults {
		if fr.SourceFile == source {
			final = fr.After
		}
	}
	return final, nil
}

// emitResult writes or previews the repaired content using the fixer's shared
// diff and atomic-write machinery.
func emitResult(source string, before, after []byte) error {
	diff := fixer.Diff(source, string(before), string(after))

	if fixDryRun {
		fmt.Print(diff)
		return nil
	}

	if fixOut != "" {
		outPath, err := fixer.OutputPath(fixOut, source)
		if err != nil {
			return err
		}
		if err := fixer.WriteAtomic(outPath, after); err != nil {
			return err
		}
		fmt.Print(diff)
		fmt.Fprintf(os.Stderr, "Wrote %s\n", outPath)
		return nil
	}

	if source == "<stdin>" {
		os.Stdout.Write(after)
		return nil
	}

	if !fixInPlace {
		fmt.Print(diff)
		fmt.Fprintf(os.Stderr, "(preview only; use --in-place to overwrite %s or --out <dir>)\n", source)
		return nil
	}

	if err := fixer.WriteAtomic(source, after); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", source)
	return nil
}

func engTargetVersion(eng *engine.Engine) string {
	if targetVersion != "" {
		return targetVersion
	}
	return kubernetesVersion
}

func readErrorText(fromFlag string, stdin bool) (string, error) {
	var parts []string
	if fromFlag != "" {
		parts = append(parts, fromFlag)
	}
	if stdin {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			parts = append(parts, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no error text provided")
	}
	return strings.Join(parts, "\n"), nil
}

func init() {
	fixCmd.Flags().BoolVar(&fixDryRun, "dry-run", false, "Show diff without writing files")
	fixCmd.Flags().StringVar(&fixOut, "out", "", "Write fixed manifests to directory")
	fixCmd.Flags().BoolVar(&fixInPlace, "in-place", false, "Overwrite source files in place")
	fixCmd.Flags().BoolVar(&fixInteractive, "interactive", true, "Review each repair before applying (auto-disabled for piped stdin)")
	fixCmd.Flags().BoolVar(&fixYes, "yes", false, "Apply all repairs without prompting (for CI/scripts)")
	fixCmd.Flags().BoolVar(&fixVerify, "verify", true, "Verify repaired manifests would apply (server dry-run, or --schema offline)")
	fixCmd.Flags().StringSliceVar(&fixIDs, "fix-id", nil, "Apply only Kubernetes findings with these rule IDs")
	fixCmd.Flags().StringVar(&fixFromError, "from-error", "", "Apply fixes matching kubectl/admission error text")
	fixCmd.Flags().BoolVar(&fixStdinErrors, "stdin-errors", false, "Read kubectl error text from stdin")
	fixCmd.Flags().BoolVar(&fixRepairSyntax, "repair-syntax", false, "(deprecated) syntax repair is always on")
	fixCmd.Flags().StringVar(&fixRepairOnly, "repair-only", "all", "Limit Kubernetes repairs: all|structure|policy")
	fixCmd.Flags().IntVar(&fixMaxRounds, "max-repair-rounds", 3, "Max verify/repair rounds when reconciling dry-run errors")
	fixCmd.Flags().BoolVar(&fixAI, "ai", false, "Enable AI-assisted repair after deterministic fixes")
	fixCmd.Flags().BoolVar(&fixAcceptRisk, "accept-risk", false, "Accept heuristic/AI repairs without confirmation")
	rootCmd.AddCommand(fixCmd)
}
