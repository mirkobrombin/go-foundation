// Package audit runs the supply chain scan that backs foundation audit.
//
// The scan is EUProvGuard, imported as a library rather than executed as a
// binary. That matters for a development tool: there is nothing to locate on
// PATH, nothing to keep in step with a separate release, and no output to parse
// back into structure. The version that ships is the version that runs.
//
// It is a separate command from foundation check on purpose. The analyzer is
// static, local and fast enough to run on every save; this reads manifests,
// queries vulnerability databases over the network, and walks the tree looking
// for patterns. Putting the two behind one command would make the fast one slow
// and the offline one dependent on the network.
package audit

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/fabricatorsltd/euprovguard/pkg/engine"
	"github.com/fabricatorsltd/euprovguard/pkg/sbom"
	"github.com/mirkobrombin/go-foundation/dev/v2/version"
)

// ToolVersion identifies Foundation, and which build of it, in the SBOM.
func ToolVersion() string {
	return "foundation " + version.Current()
}

// Options configures an audit.
type Options struct {
	// Directory is the project root. Defaults to the working directory.
	Directory string
	// Name overrides the project name recorded in the SBOM.
	Name string
	// Version is the project version recorded in the SBOM.
	Version string
	// Offline skips every network step. The result is always degraded,
	// because an offline scan cannot know about anything disclosed since
	// the binary was built.
	Offline bool
	// SkipSAST turns off the static analysis pass.
	SkipSAST bool
	// SBOMPath writes the CycloneDX document when set.
	SBOMPath string
}

// Finding is one thing the scan reported, from either source.
type Finding struct {
	// Kind is "vulnerability" or "code".
	Kind string `json:"kind"`
	// ID is the CVE identifier, or the rule that matched.
	ID string `json:"id"`
	// Severity as reported by the source, lowercased.
	Severity string `json:"severity"`
	// Subject is the affected dependency, or the file and line.
	Subject string `json:"subject"`
	// Description is the finding text.
	Description string `json:"description,omitempty"`
}

// Result is the outcome of one audit.
type Result struct {
	Directory      string         `json:"directory"`
	Dependencies   int            `json:"dependencies"`
	Components     int            `json:"components"`
	Findings       []Finding      `json:"findings"`
	SeverityCounts map[string]int `json:"severity_counts,omitempty"`
	// Degraded lists the reasons this scan is incomplete. It is the
	// difference between "nothing was found" and "nothing could be looked
	// for", and it is why Passed is not simply a finding count.
	Degraded []string `json:"degraded,omitempty"`
	Passed   bool     `json:"passed"`
	Summary  string   `json:"summary"`
	SBOMPath string   `json:"sbom_path,omitempty"`
	// Output is what the scan printed while running, kept so a caller can
	// show it without it leaking into its own logs.
	Output string `json:"output,omitempty"`
}

// captureMutex guards the standard logger while a scan runs. The scanner
// packages log through it, which is fine for a command line tool and wrong for
// a server, so the output is captured and returned instead of printed.
var captureMutex sync.Mutex

// Run performs the audit and reports what it found, and what it could not.
func Run(ctx context.Context, opts Options) (*Result, error) {
	directory := opts.Directory
	if strings.TrimSpace(directory) == "" {
		directory = "."
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("audit: resolve %q: %w", directory, err)
	}

	captureMutex.Lock()
	var captured bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&captured)

	scan, runErr := engine.Run(ctx, engine.Options{
		Path:           root,
		ProjectName:    opts.Name,
		ProjectVersion: opts.Version,
		ToolVersion:    ToolVersion(),
		Offline:        opts.Offline,
		DisableSAST:    opts.SkipSAST,
	})

	log.SetOutput(previousWriter)
	log.SetFlags(previousFlags)
	log.SetPrefix(previousPrefix)
	captureMutex.Unlock()

	if runErr != nil {
		return nil, fmt.Errorf("audit: %w", runErr)
	}

	result := &Result{
		Directory:      root,
		Dependencies:   len(scan.Dependencies),
		SeverityCounts: scan.SeverityCounts,
		Degraded:       scan.Degraded,
		Output:         strings.TrimSpace(captured.String()),
	}
	if scan.BOM != nil {
		result.Components = len(scan.BOM.Components)
	}

	for _, finding := range scan.Vulnerabilities {
		result.Findings = append(result.Findings, Finding{
			Kind:        "vulnerability",
			ID:          finding.CVE.ID,
			Severity:    strings.ToLower(string(finding.CVE.Severity)),
			Subject:     finding.Component,
			Description: finding.CVE.Description,
		})
	}
	for _, finding := range scan.SAST {
		result.Findings = append(result.Findings, Finding{
			Kind:        "code",
			ID:          finding.RuleID,
			Severity:    strings.ToLower(finding.Severity),
			Subject:     fmt.Sprintf("%s:%d", finding.File, finding.Line),
			Description: finding.Description,
		})
	}
	sort.SliceStable(result.Findings, func(i, j int) bool {
		if result.Findings[i].Kind != result.Findings[j].Kind {
			return result.Findings[i].Kind < result.Findings[j].Kind
		}
		return result.Findings[i].ID < result.Findings[j].ID
	})

	if opts.SBOMPath != "" && scan.BOM != nil {
		if err := sbom.WriteJSON(scan.BOM, opts.SBOMPath); err != nil {
			return nil, fmt.Errorf("audit: write SBOM: %w", err)
		}
		result.SBOMPath = opts.SBOMPath
	}

	result.Passed = len(result.Findings) == 0 && len(result.Degraded) == 0
	result.Summary = summarise(result)
	return result, nil
}

func summarise(result *Result) string {
	switch {
	case result.Passed:
		return fmt.Sprintf(
			"%d dependencies scanned, no vulnerabilities and no code findings",
			result.Dependencies,
		)
	case len(result.Findings) > 0 && len(result.Degraded) > 0:
		return fmt.Sprintf(
			"%d finding(s), and the scan was incomplete: %s",
			len(result.Findings), strings.Join(result.Degraded, "; "),
		)
	case len(result.Findings) > 0:
		return fmt.Sprintf("%d finding(s) across %d dependencies",
			len(result.Findings), result.Dependencies)
	default:
		return "no findings, but the scan was incomplete, so this is not an all clear: " +
			strings.Join(result.Degraded, "; ")
	}
}

// Report writes a human readable summary.
func Report(writer io.Writer, result *Result) {
	fmt.Fprintf(writer, "%s\n", result.Summary)
	for _, finding := range result.Findings {
		fmt.Fprintf(writer, "  %-13s %-8s %s  %s\n",
			finding.Kind, finding.Severity, finding.ID, finding.Subject)
	}
	for _, reason := range result.Degraded {
		fmt.Fprintf(writer, "  incomplete:   %s\n", reason)
	}
	if result.SBOMPath != "" {
		fmt.Fprintf(writer, "  sbom:         %s\n", result.SBOMPath)
	}
}
