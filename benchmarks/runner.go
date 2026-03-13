package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type BenchmarkResult struct {
	Name         string
	PythonTime   float64
	PolyloftTime float64
	Speedup      float64
	Passed       bool
}

func parseOutput(outBytes []byte, marker string) (string, float64, error) {
	lines := strings.Split(strings.TrimSpace(string(outBytes)), "\n")
	outputLines := []string{}
	timeVal := 0.0

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == marker {
			if i+1 < len(lines) {
				t, err := strconv.ParseFloat(strings.TrimSpace(lines[i+1]), 64)
				if err == nil {
					timeVal = t
				}
				i++ // skip the time value line
			}
		} else {
			outputLines = append(outputLines, line)
		}
	}
	return strings.Join(outputLines, "\n"), timeVal, nil
}

func main() {
	fmt.Println("Building Polyloft BVM for benchmarks...")
	cmd := exec.Command("go", "build", "-o", "polyloft-bvm.exe", "../cmd/polyloft-bvm")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to build polyloft-bvm: %v\n", err)
		return
	}

	testFiles, err := filepath.Glob("tests/*.pf")
	if err != nil {
		fmt.Println("Error finding tests:", err)
		return
	}
	if len(testFiles) == 0 {
		fmt.Println("No tests found in benchmarks/tests/")
		return
	}

	sort.Strings(testFiles)

	fmt.Println("Running benchmarks...")
	fmt.Println("---------------------------------------------------------")

	results := []BenchmarkResult{}
	var totalPy float64
	var totalPf float64
	passedCount := 0

	for _, pfPath := range testFiles {
		baseName := strings.TrimSuffix(filepath.Base(pfPath), ".pf")
		pyPath := filepath.Join("tests", baseName+".py")
		fmt.Printf("Benchmarking %s...\n", baseName)

		pyCmd := exec.Command("python", pyPath)
		var pyOut bytes.Buffer
		pyCmd.Stdout = &pyOut
		pyCmd.Stderr = &pyOut
		pyErr := pyCmd.Run()
		pyOutputRaw := pyOut.String()

		pfCmd := exec.Command("./polyloft-bvm.exe", "run", "--jit", pfPath)
		var pfOut bytes.Buffer
		pfCmd.Stdout = &pfOut
		pfCmd.Stderr = &pfOut
		pfErr := pfCmd.Run()
		pfOutputRaw := pfOut.String()

		pyOutput, pyTime, _ := parseOutput([]byte(pyOutputRaw), "TIME_PY")
		pfOutput, pfTime, _ := parseOutput([]byte(pfOutputRaw), "TIME_PF")

		passed := false
		if pfErr != nil {
			fmt.Printf("  [PF ERROR] %v\n", pfErr)
		}
		if pyErr != nil {
			fmt.Printf("  [PY ERROR] %v\n", pyErr)
		}

		if pyOutput == pfOutput && pfErr == nil && pyErr == nil {
			passed = true
			passedCount++
			totalPy += pyTime
			totalPf += pfTime
		} else {
			fmt.Printf("  [MISMATCH]\n")
			fmt.Printf("  Python: %q\n", pyOutput)
			fmt.Printf("  Polyloft: %q\n", pfOutput)
		}

		speedup := 0.0
		if pfTime > 0 {
			speedup = pyTime / pfTime
		}

		results = append(results, BenchmarkResult{
			Name:         baseName,
			PythonTime:   pyTime,
			PolyloftTime: pfTime,
			Speedup:      speedup,
			Passed:       passed,
		})
	}

	report := generateReport(results, passedCount, len(testFiles), totalPy, totalPf)
	err = ioutil.WriteFile("benchmark_report.md", []byte(report), 0644)
	if err != nil {
		fmt.Println("Error writing report:", err)
	} else {
		fmt.Println("Wrote detail report to benchmarks/benchmark_report.md")
	}
}

func generateReport(results []BenchmarkResult, passed, total int, totalPy, totalPf float64) string {
	overallSpeedup := 0.0
	if totalPf > 0 {
		overallSpeedup = totalPy / totalPf
	}

	report := "# Polyloft BVM vs Python Stress Test Benchmark\n\n"
	report += fmt.Sprintf("## Summary\n")
	report += fmt.Sprintf("- **Tests Passed:** %d / %d\n", passed, total)
	report += fmt.Sprintf("- **Total Python Internal Time (passed):** %.0f ms\n", totalPy)
	report += fmt.Sprintf("- **Total Polyloft Internal Time (passed):** %.0f ms\n", totalPf)
	report += fmt.Sprintf("- **Overall Speedup:** %.2fx\n\n", overallSpeedup)

	report += "## Detailed Results\n"
	report += "| Test Name | Python Time (ms) | Polyloft Time (ms) | Speedup | Passed |\n"
	report += "|-----------|------------------|--------------------|---------|--------|\n"

	for _, r := range results {
		passStr := "❌"
		if r.Passed {
			passStr = "✅"
		}
		report += fmt.Sprintf("| %-30s | %13.0f | %15.0f | %6.2fx |   %s   |\n",
			r.Name, r.PythonTime, r.PolyloftTime, r.Speedup, passStr)
	}

	return report
}
