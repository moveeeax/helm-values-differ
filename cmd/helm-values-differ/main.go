// Command helm-values-differ renders a Helm chart with two different value sets
// (via `helm template`) and prints the semantic diff of the resulting
// Kubernetes manifests — grouped by object, field by field — instead of a
// misleading line diff of values.yaml.
//
//	helm-values-differ ./chart -f base.yaml -f candidate.yaml
//
// Each -f before the separator applies to the base render; each -f after applies
// to the candidate. If no separator is given, the first -f is base and the
// second is candidate.
//
// Exit codes:
//
//	0  no manifest changes
//	1  manifest changes found
//	2  runtime error (helm missing, bad chart, parse failure)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/moveeeax/helm-values-differ/internal/diff"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("helm-values-differ", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var baseVals multiFlag
	var candVals multiFlag
	fs.Var(&baseVals, "base", "value file for the BASE render (repeatable)")
	fs.Var(&candVals, "candidate", "value file for the CANDIDATE render (repeatable)")
	format := fs.String("format", "text", "output format: text | markdown | json")
	helmBin := fs.String("helm", "helm", "path to the helm binary")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: helm-values-differ CHART --base base.yaml --candidate candidate.yaml [--format text|markdown|json]\n\n")
		fmt.Fprintf(stderr, "Renders CHART twice with helm template and diffs the resulting manifests.\n\n")
		fs.PrintDefaults()
	}
	// The chart path is positional and, per the documented usage, comes first
	// (`helm-values-differ ./chart --base ... --candidate ...`). Go's flag
	// package stops at the first non-flag token, so lift the chart out before
	// parsing the remaining flags.
	chart, flagArgs := splitChart(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if extra := fs.Args(); len(extra) > 0 {
		fmt.Fprintf(stderr, "error: unexpected argument %q\n\n", extra[0])
		fs.Usage()
		return 2
	}
	if chart == "" {
		fmt.Fprintf(stderr, "error: a CHART path is required\n\n")
		fs.Usage()
		return 2
	}
	if len(baseVals) == 0 || len(candVals) == 0 {
		fmt.Fprintf(stderr, "error: at least one --base and one --candidate value file are required\n")
		return 2
	}

	res, err := diff.RenderAndDiff(diff.HelmCLI{Bin: *helmBin}, chart, baseVals, candVals)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	var werr error
	switch *format {
	case "markdown":
		werr = res.WriteMarkdown(stdout)
	case "json":
		werr = res.WriteJSON(stdout)
	case "text":
		werr = res.WriteText(stdout)
	default:
		fmt.Fprintf(stderr, "error: unknown format %q (want text|markdown|json)\n", *format)
		return 2
	}
	if werr != nil {
		fmt.Fprintf(stderr, "error: %v\n", werr)
		return 2
	}

	if !res.Empty() {
		return 1
	}
	return 0
}

// splitChart returns the first bare positional token (the chart path) and the
// remaining args with that token removed, so flags may appear before or after
// the chart. A token is a flag if it begins with "-".
func splitChart(args []string) (string, []string) {
	for i, a := range args {
		if len(a) > 0 && a[0] == '-' {
			continue
		}
		// Skip a value that belongs to a preceding space-separated flag
		// (e.g. `--base file`): if the previous token is a known value flag,
		// this token is its argument, not the chart.
		if i > 0 && isValueFlag(args[i-1]) {
			continue
		}
		rest := append(append([]string{}, args[:i]...), args[i+1:]...)
		return a, rest
	}
	return "", args
}

// isValueFlag reports whether flag consumes a following space-separated value.
func isValueFlag(flag string) bool {
	switch flag {
	case "-base", "--base", "-candidate", "--candidate", "-format", "--format", "-helm", "--helm":
		return true
	}
	return false
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string { return fmt.Sprintf("%v", []string(*m)) }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}
