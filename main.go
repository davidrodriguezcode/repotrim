package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"repotrim/core"
)

func main() {
	// Flags
	dirFlag := flag.String("dir", ".", "Root directory to scan and analyze")
	workersFlag := flag.Int("workers", 4, "Number of concurrent worker goroutines")
	ignoreFlag := flag.String("ignore", "", "Comma-separated custom ignore patterns")
	formatFlag := flag.String("format", "text", "Output format (text, json)")
	flag.Parse()

	// Licensing Check
	licenseKey := os.Getenv("REPOTRIM_LICENSE_KEY")
	if licenseKey == "" {
		fmt.Fprintln(os.Stderr, "Error: REPOTRIM_LICENSE_KEY environment variable is not set.")
		fmt.Fprintln(os.Stderr, "A valid license key is required to run RepoTrim in CI/CD pipelines.")
		fmt.Fprintln(os.Stderr, "Please purchase a license key at https://repotrim.com to proceed.")
		os.Exit(1)
	}

	valid, err := core.VerifyLicense(licenseKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Licensing Verification Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Please check your network connection or contact support@repotrim.com.")
		os.Exit(1)
	}

	if !valid {
		fmt.Fprintln(os.Stderr, "Error: The provided REPOTRIM_LICENSE_KEY is invalid, forbidden, or expired.")
		fmt.Fprintln(os.Stderr, "Please verify your key or renew your subscription at https://repotrim.com.")
		os.Exit(1)
	}

	startTime := time.Now()

	// Get absolute target directory
	rootDir, err := filepath.Abs(*dirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving absolute path: %v\n", err)
		os.Exit(1)
	}

	// Verify directory exists
	info, err := os.Stat(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Directory '%s' does not exist\n", rootDir)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: Path '%s' is a file, not a directory\n", rootDir)
		os.Exit(1)
	}

	// Prepare ignore patterns
	var customIgnores []string
	if *ignoreFlag != "" {
		customIgnores = strings.Split(*ignoreFlag, ",")
		for i, p := range customIgnores {
			customIgnores[i] = strings.TrimSpace(p)
		}
	}

	// 1. Scan files
	scannerConf := core.ScannerConfig{
		RootDir:        rootDir,
		Workers:        *workersFlag,
		IgnorePatterns: customIgnores,
	}
	scanner := core.NewScanner(scannerConf)
	assets, err := scanner.Scan()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Scan error: %v\n", err)
		os.Exit(1)
	}

	// 2. Parse references
	parser := core.NewParser(*workersFlag)
	references, err := parser.Parse(assets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Parsing error: %v\n", err)
		os.Exit(1)
	}

	// 3. Analyze issues
	analyzer := core.NewAnalyzer(assets, references)
	durationMs := time.Since(startTime).Milliseconds()
	report := analyzer.Analyze(rootDir, durationMs)

	// 4. Output results
	if strings.ToLower(*formatFlag) == "json" {
		printJSON(report)
	} else {
		printText(report)
	}
}

func printJSON(report core.AnalysisReport) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON encoding error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func printText(report core.AnalysisReport) {
	fmt.Println("================================================================================")
	fmt.Printf("📂 RepoTrim Report: %s\n", report.RootDir)
	fmt.Println("================================================================================")
	fmt.Printf("📊 Metrics:\n")
	fmt.Printf("   • Files Scanned:       %d\n", report.TotalFilesScanned)
	fmt.Printf("   • Bytes Analyzed:      %s\n", formatBytes(report.TotalBytesScanned))
	fmt.Printf("   • Duration:            %d ms\n", report.ExecutionTimeMs)
	fmt.Printf("   • Issues Found:        %d\n", report.TotalIssuesFound)
	fmt.Printf("   • Total Bloat Savings: %s\n", formatBytes(report.TotalSavingsBytes))
	fmt.Println("================================================================================")

	if len(report.Issues) == 0 {
		fmt.Println("🎉 Excellent! No structural bloat, duplicates, or dead assets were detected.")
		fmt.Println("================================================================================")
		return
	}

	fmt.Println("⚠️  Optimization Findings:")
	fmt.Println()

	for i, issue := range report.Issues {
		fmt.Printf("[%d] %s\n", i+1, strings.ToUpper(string(issue.Type)))
		fmt.Printf("    File:   %s\n", issue.FilePath)
		if issue.SavingsBytes > 0 {
			fmt.Printf("    Bloat:  %s\n", formatBytes(issue.SavingsBytes))
		}
		fmt.Printf("    Reason: %s\n", issue.Details)
		fmt.Printf("    Action: %s\n", issue.Recommendation)
		fmt.Println()
	}

	fmt.Println("================================================================================")
	fmt.Printf("🚀 Total Potential Repository Size Reduction: %s\n", formatBytes(report.TotalSavingsBytes))
	fmt.Println("================================================================================")
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
