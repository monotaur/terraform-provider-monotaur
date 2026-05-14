// Package main implements the e2e-summary command, which reads
// e2e-results/raw.jsonl (go test -json output) and writes
// e2e-results/summary.md in a machine- and human-readable format.
//
// Usage: go run ./internal/acctest/summary/ [--input <path>] [--output <path>]
//
// The first line of the output is the verdict:
//
//	Result: PASS
//	Result: FAIL (n failures)
//
// Claude: after `make e2e`, read e2e-results/summary.md. If the first line is
// "Result: PASS", the suite passed. Otherwise, read the per-failure sections.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// testEvent mirrors the go test -json event format.
type testEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed float64   `json:"Elapsed"`
	Output  string    `json:"Output"`
}

type testResult struct {
	pkg      string
	test     string
	action   string // "pass", "fail", "skip"
	elapsed  float64
	output   []string
}

type pkgSummary struct {
	pkg     string
	passed  int
	failed  int
	skipped int
	elapsed float64
}

func main() {
	input := flag.String("input", "e2e-results/raw.jsonl", "path to go test -json output file")
	output := flag.String("output", "e2e-results/summary.md", "path to write summary.md")
	flag.Parse()

	f, err := os.Open(*input)
	if err != nil {
		// Write a failure summary if the input file is missing.
		writeSummary(*output, "FAIL (input file missing: "+err.Error()+")", nil, nil, 0)
		os.Exit(1)
	}
	defer f.Close()

	// results maps "pkg/TestName" → result
	results := map[string]*testResult{}
	// outputBuf collects output lines per test key
	outputBuf := map[string][]string{}
	// pkgElapsed tracks elapsed time per package (from the package-level event)
	pkgElapsed := map[string]float64{}

	var totalElapsed float64

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		key := ev.Package + "/" + ev.Test

		switch ev.Action {
		case "run":
			results[key] = &testResult{pkg: ev.Package, test: ev.Test}
		case "output":
			if ev.Test != "" {
				outputBuf[key] = append(outputBuf[key], ev.Output)
			}
		case "pass", "fail", "skip":
			if ev.Test == "" {
				// Package-level result
				pkgElapsed[ev.Package] = ev.Elapsed
				totalElapsed += ev.Elapsed
				continue
			}
			r := results[key]
			if r == nil {
				r = &testResult{pkg: ev.Package, test: ev.Test}
				results[key] = r
			}
			r.action = ev.Action
			r.elapsed = ev.Elapsed
			r.output = outputBuf[key]
		}
	}

	// Collect per-package stats
	pkgMap := map[string]*pkgSummary{}
	var failures []*testResult

	for _, r := range results {
		ps := pkgMap[r.pkg]
		if ps == nil {
			ps = &pkgSummary{pkg: r.pkg, elapsed: pkgElapsed[r.pkg]}
			pkgMap[r.pkg] = ps
		}
		switch r.action {
		case "pass":
			ps.passed++
		case "fail":
			ps.failed++
			failures = append(failures, r)
		case "skip":
			ps.skipped++
		}
	}

	// Sort packages and failures for deterministic output
	var pkgs []*pkgSummary
	for _, ps := range pkgMap {
		pkgs = append(pkgs, ps)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].pkg < pkgs[j].pkg })
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].pkg != failures[j].pkg {
			return failures[i].pkg < failures[j].pkg
		}
		return failures[i].test < failures[j].test
	})

	var verdict string
	if len(failures) == 0 {
		verdict = "PASS"
	} else {
		verdict = fmt.Sprintf("FAIL (%d failure", len(failures))
		if len(failures) != 1 {
			verdict += "s"
		}
		verdict += ")"
	}

	writeSummary(*output, verdict, pkgs, failures, totalElapsed)

	if len(failures) > 0 {
		os.Exit(1)
	}
}

func writeSummary(path, verdict string, pkgs []*pkgSummary, failures []*testResult, totalElapsed float64) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e-summary: cannot write %s: %v\n", path, err)
		return
	}
	defer f.Close()

	w := bufio.NewWriter(f)

	// Verdict — first line is the machine-readable contract
	fmt.Fprintf(w, "Result: %s\n\n", verdict)

	// Counts table
	var total, passed, failed, skipped int
	for _, ps := range pkgs {
		passed += ps.passed
		failed += ps.failed
		skipped += ps.skipped
	}
	total = passed + failed + skipped

	fmt.Fprintf(w, "## Summary\n\n")
	fmt.Fprintf(w, "| Metric   | Count |\n")
	fmt.Fprintf(w, "|----------|-------|\n")
	fmt.Fprintf(w, "| Total    | %d |\n", total)
	fmt.Fprintf(w, "| Passed   | %d |\n", passed)
	fmt.Fprintf(w, "| Failed   | %d |\n", failed)
	fmt.Fprintf(w, "| Skipped  | %d |\n", skipped)
	fmt.Fprintf(w, "| Duration | %.1fs |\n\n", totalElapsed)

	// Per-package breakdown
	if len(pkgs) > 0 {
		fmt.Fprintf(w, "## Packages\n\n")
		fmt.Fprintf(w, "| Package | Pass | Fail | Skip | Duration |\n")
		fmt.Fprintf(w, "|---------|------|------|------|----------|\n")
		for _, ps := range pkgs {
			fmt.Fprintf(w, "| `%s` | %d | %d | %d | %.1fs |\n",
				ps.pkg, ps.passed, ps.failed, ps.skipped, ps.elapsed)
		}
		fmt.Fprintln(w)
	}

	// Per-failure sections
	if len(failures) > 0 {
		fmt.Fprintf(w, "## Failures\n\n")
		for i, r := range failures {
			fmt.Fprintf(w, "### %d. `%s`\n\n", i+1, r.test)
			fmt.Fprintf(w, "**Package:** `%s`  \n", r.pkg)
			fmt.Fprintf(w, "**Duration:** %.1fs\n\n", r.elapsed)
			fmt.Fprintf(w, "```\n")
			// Emit last 50 lines of output
			lines := r.output
			if len(lines) > 50 {
				fmt.Fprintf(w, "... (truncated, showing last 50 lines)\n")
				lines = lines[len(lines)-50:]
			}
			for _, l := range lines {
				fmt.Fprint(w, l)
			}
			fmt.Fprintf(w, "```\n\n")
		}
	}

	w.Flush()
}
