package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"repotrim/core"
)

// Spinner manages the CLI loading indicator.
type Spinner struct {
	mu       sync.Mutex
	active   bool
	message  string
	stopChan chan struct{}
}

func NewSpinner(msg string) *Spinner {
	return &Spinner{
		message:  msg,
		stopChan: make(chan struct{}),
	}
}

func (s *Spinner) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	s.mu.Unlock()

	isTerminal := false
	fileInfo, err := os.Stdout.Stat()
	if err == nil && (fileInfo.Mode()&os.ModeCharDevice) != 0 {
		isTerminal = true
	}

	if !isTerminal {
		fmt.Println(s.message + "...")
		return
	}

	go func() {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-s.stopChan:
				fmt.Print("\r\033[K")
				return
			default:
				fmt.Printf("\r\033[36m%s\033[0m %s...", frames[i], s.message)
				i = (i + 1) % len(frames)
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	s.mu.Unlock()

	isTerminal := false
	fileInfo, err := os.Stdout.Stat()
	if err == nil && (fileInfo.Mode()&os.ModeCharDevice) != 0 {
		isTerminal = true
	}

	if isTerminal {
		s.stopChan <- struct{}{}
	}
}

func main() {
	// Flags
	dirFlag := flag.String("dir", ".", "Root directory to scan and analyze")
	workersFlag := flag.Int("workers", 4, "Number of concurrent worker goroutines")
	ignoreFlag := flag.String("ignore", "", "Comma-separated custom ignore patterns")
	formatFlag := flag.String("format", "text", "Output format (text, json)")
	fixFlag := flag.Bool("fix", false, "Interactively resolve duplicate assets, unused assets, and empty directories")
	forceFlag := flag.Bool("force", false, "Auto-apply fixes without interactive prompts")
	dryRunFlag := flag.Bool("dry-run", false, "Simulate pruning operations without modifying files")
	reportFileFlag := flag.String("report-file", "", "Path to save JSON report (includes actions taken if in fix mode)")
	verifyCmdFlag := flag.String("verify-cmd", "", "Custom validation build/test command to run after fixes (rolls back if it fails)")
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

	scanSpinner := NewSpinner("Scanning workspace assets")
	scanSpinner.Start()
	assets, err := scanner.Scan()
	scanSpinner.Stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Scan error: %v\n", err)
		os.Exit(1)
	}

	// 2. Parse references
	parser := core.NewParser(*workersFlag)

	parseSpinner := NewSpinner("Parsing codebase references")
	parseSpinner.Start()
	references, err := parser.Parse(assets)
	parseSpinner.Stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Parsing error: %v\n", err)
		os.Exit(1)
	}

	// 3. Analyze issues
	analyzeSpinner := NewSpinner("Analyzing structural bloat")
	analyzeSpinner.Start()
	analyzer := core.NewAnalyzer(assets, references)
	durationMs := time.Since(startTime).Milliseconds()
	report := analyzer.Analyze(rootDir, durationMs)
	analyzeSpinner.Stop()

	// 4. Backups & Pruning Actions
	var actionsTaken []core.ActionLog
	var actionsSimulated []core.ActionLog

	if *fixFlag || *dryRunFlag {
		if len(report.Issues) == 0 {
			fmt.Println("No issues found to resolve!")
		} else {
			if *fixFlag && !*forceFlag && !isTerminal() {
				fmt.Fprintln(os.Stderr, "Error: Standard input is not a terminal. Use -force to apply fixes in non-interactive environments.")
				os.Exit(1)
			}

			var backupDir string
			if *fixFlag && !*dryRunFlag {
				backupDir, err = os.MkdirTemp("", "repotrim-backup-*")
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating backup directory: %v\n", err)
					os.Exit(1)
				}
				defer os.RemoveAll(backupDir)
			}

			backedUpFiles := make(map[string]string)

			backupFile := func(relPath string) error {
				origPath := filepath.Join(rootDir, relPath)
				if _, exists := backedUpFiles[origPath]; exists {
					return nil
				}
				backupPath := filepath.Join(backupDir, relPath)
				if err := copyFile(origPath, backupPath); err != nil {
					return err
				}
				backedUpFiles[origPath] = backupPath
				return nil
			}

			rollback := func() {
				fmt.Println("\n⚠️  Rollback initiated. Restoring codebase state...")
				for origPath, backupPath := range backedUpFiles {
					if err := copyFile(backupPath, origPath); err != nil {
						fmt.Fprintf(os.Stderr, "Error restoring file %s: %v\n", origPath, err)
					}
				}
				gitCheck := exec.Command("git", "rev-parse", "--is-inside-work-tree")
				gitCheck.Dir = rootDir
				if err := gitCheck.Run(); err == nil {
					discardCmd := exec.Command("git", "checkout", "--", ".")
					discardCmd.Dir = rootDir
					discardCmd.Run()
				}
				fmt.Println("✅ Rollback complete. Workspace restored to original state.")
			}

			for _, issue := range report.Issues {
				absPath := filepath.Join(rootDir, issue.FilePath)

				switch issue.Type {
				case core.UnusedAsset, core.UnusedConfig:
					prompt := fmt.Sprintf("Delete unused file '%s'?", issue.FilePath)
					if *dryRunFlag {
						actionsSimulated = append(actionsSimulated, core.ActionLog{
							Action:  "delete",
							Path:    issue.FilePath,
							Details: "[Simulated] Would delete unused file",
						})
					} else if *forceFlag || askConfirm(prompt) {
						if err := backupFile(issue.FilePath); err != nil {
							fmt.Fprintf(os.Stderr, "Backup error: %v\n", err)
							rollback()
							os.Exit(1)
						}
						if err := os.Remove(absPath); err != nil {
							fmt.Fprintf(os.Stderr, "Error deleting file: %v\n", err)
							rollback()
							os.Exit(1)
						}
						actionsTaken = append(actionsTaken, core.ActionLog{
							Action:  "delete",
							Path:    issue.FilePath,
							Details: "Deleted unused file",
						})
						fmt.Printf("🗑️  Deleted: %s\n", issue.FilePath)
					}

				case core.DuplicateAsset:
					canonicalRelPath := ""
					parts := strings.Split(issue.Details, "'")
					if len(parts) > 1 {
						canonicalRelPath = parts[1]
					}

					prompt := fmt.Sprintf("Delete duplicate file '%s' (canonical: '%s')?", issue.FilePath, canonicalRelPath)
					if *dryRunFlag {
						actionsSimulated = append(actionsSimulated, core.ActionLog{
							Action:  "delete",
							Path:    issue.FilePath,
							Details: fmt.Sprintf("[Simulated] Would delete duplicate file (canonical: '%s')", canonicalRelPath),
						})
						if canonicalRelPath != "" {
							replaceReferencesInCode(assets, issue.FilePath, canonicalRelPath, &actionsSimulated, true)
						}
					} else if *forceFlag || askConfirm(prompt) {
						if err := backupFile(issue.FilePath); err != nil {
							fmt.Fprintf(os.Stderr, "Backup error: %v\n", err)
							rollback()
							os.Exit(1)
						}
						if err := os.Remove(absPath); err != nil {
							fmt.Fprintf(os.Stderr, "Error deleting duplicate: %v\n", err)
							rollback()
							os.Exit(1)
						}
						actionsTaken = append(actionsTaken, core.ActionLog{
							Action:  "delete",
							Path:    issue.FilePath,
							Details: fmt.Sprintf("Deleted duplicate file (canonical: '%s')", canonicalRelPath),
						})
						fmt.Printf("🗑️  Deleted duplicate: %s\n", issue.FilePath)

						if canonicalRelPath != "" {
							replaceReferencesInCodeWithBackup(assets, issue.FilePath, canonicalRelPath, &actionsTaken, backupFile)
						}
					}

				case core.EmptyDirectory:
					prompt := fmt.Sprintf("Delete empty directory '%s'?", issue.FilePath)
					if *dryRunFlag {
						actionsSimulated = append(actionsSimulated, core.ActionLog{
							Action:  "remove_dir",
							Path:    issue.FilePath,
							Details: "[Simulated] Would remove empty directory",
						})
					} else if *forceFlag || askConfirm(prompt) {
						if err := os.Remove(absPath); err != nil {
							fmt.Fprintf(os.Stderr, "Error removing empty directory: %v\n", err)
							rollback()
							os.Exit(1)
						}
						actionsTaken = append(actionsTaken, core.ActionLog{
							Action:  "remove_dir",
							Path:    issue.FilePath,
							Details: "Removed empty directory",
						})
						fmt.Printf("📁 Removed empty directory: %s\n", issue.FilePath)
					}

				case core.TrackedIgnoredFile:
					prompt := fmt.Sprintf("Untrack ignored file '%s' from git?", issue.FilePath)
					if *dryRunFlag {
						actionsSimulated = append(actionsSimulated, core.ActionLog{
							Action:  "git_rm_cached",
							Path:    issue.FilePath,
							Details: "[Simulated] Would untrack ignored file",
						})
					} else if *forceFlag || askConfirm(prompt) {
						cmd := exec.Command("git", "rm", "--cached", issue.FilePath)
						cmd.Dir = rootDir
						if err := cmd.Run(); err != nil {
							fmt.Fprintf(os.Stderr, "Error untracking file from git: %v\n", err)
							rollback()
							os.Exit(1)
						}
						actionsTaken = append(actionsTaken, core.ActionLog{
							Action:  "git_rm_cached",
							Path:    issue.FilePath,
							Details: "Untracked ignored file from git index",
						})
						fmt.Printf("🐙 Untracked from git: %s\n", issue.FilePath)
					}
				}
			}

			// Run Verification Command
			if *fixFlag && !*dryRunFlag && *verifyCmdFlag != "" {
				fmt.Printf("\n🧪 Running verification command: %s...\n", *verifyCmdFlag)
				var verifyCmd *exec.Cmd
				if isWindows() {
					verifyCmd = exec.Command("cmd", "/C", *verifyCmdFlag)
				} else {
					verifyCmd = exec.Command("sh", "-c", *verifyCmdFlag)
				}
				verifyCmd.Dir = rootDir
				verifyCmd.Stdout = os.Stdout
				verifyCmd.Stderr = os.Stderr

				if err := verifyCmd.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "\n❌ Verification command failed: %v\n", err)
					rollback()
					os.Exit(1)
				} else {
					fmt.Println("✅ Verification command passed successfully!")
				}
			}
		}
	}

	report.ActionsTaken = actionsTaken
	report.ActionsSimulated = actionsSimulated

	// Save report if requested
	if *reportFileFlag != "" {
		reportPath := *reportFileFlag
		if !filepath.IsAbs(reportPath) {
			reportPath = filepath.Join(rootDir, reportPath)
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "JSON encoding error: %v\n", err)
		} else {
			err = os.WriteFile(reportPath, data, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error writing report file: %v\n", err)
			} else {
				fmt.Printf("💾 Report exported to: %s\n", reportPath)
			}
		}
	}

	// 5. Output results
	if strings.ToLower(*formatFlag) == "json" {
		printJSON(report)
	} else {
		printText(report)
	}
}

func askConfirm(prompt string) bool {
	fmt.Printf("%s (y/n): ", prompt)
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	text = strings.TrimSpace(strings.ToLower(text))
	return text == "y" || text == "yes"
}

func isTerminal() bool {
	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

func isWindows() bool {
	return runtime.GOOS == "windows"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func replaceReferencesInCode(assets []core.Asset, dupRelPath, canonicalRelPath string, actions *[]core.ActionLog, dryRun bool) {
	dupBase := filepath.Base(dupRelPath)
	canonBase := filepath.Base(canonicalRelPath)
	dupName := strings.TrimSuffix(dupBase, filepath.Ext(dupBase))
	canonName := strings.TrimSuffix(canonBase, filepath.Ext(canonBase))

	if dupRelPath == canonicalRelPath {
		return
	}

	searchStrings := []string{dupRelPath}
	replaceStrings := []string{canonicalRelPath}

	if dupBase != canonBase {
		searchStrings = append(searchStrings, dupBase)
		replaceStrings = append(replaceStrings, canonBase)
	}
	if dupName != canonName {
		searchStrings = append(searchStrings, dupName)
		replaceStrings = append(replaceStrings, canonName)
	}

	for _, asset := range assets {
		if asset.RelPath == dupRelPath || asset.RelPath == canonicalRelPath {
			continue
		}
		ext := strings.ToLower(filepath.Ext(asset.Path))
		if isBinaryFile(ext) {
			continue
		}

		content, err := os.ReadFile(asset.Path)
		if err != nil {
			continue
		}

		origContent := string(content)
		newContent := origContent
		replacedAny := false

		for i, searchStr := range searchStrings {
			if strings.Contains(newContent, searchStr) {
				newContent = strings.ReplaceAll(newContent, searchStr, replaceStrings[i])
				replacedAny = true
			}
		}

		if replacedAny {
			if dryRun {
				*actions = append(*actions, core.ActionLog{
					Action:  "replace_reference",
					Path:    asset.RelPath,
					Target:  canonicalRelPath,
					Details: fmt.Sprintf("[Simulated] Would replace references of '%s' with '%s'", dupRelPath, canonicalRelPath),
				})
			}
		}
	}
}

func replaceReferencesInCodeWithBackup(assets []core.Asset, dupRelPath, canonicalRelPath string, actions *[]core.ActionLog, backupFile func(string) error) {
	dupBase := filepath.Base(dupRelPath)
	canonBase := filepath.Base(canonicalRelPath)
	dupName := strings.TrimSuffix(dupBase, filepath.Ext(dupBase))
	canonName := strings.TrimSuffix(canonBase, filepath.Ext(canonBase))

	if dupRelPath == canonicalRelPath {
		return
	}

	searchStrings := []string{dupRelPath}
	replaceStrings := []string{canonicalRelPath}

	if dupBase != canonBase {
		searchStrings = append(searchStrings, dupBase)
		replaceStrings = append(replaceStrings, canonBase)
	}
	if dupName != canonName {
		searchStrings = append(searchStrings, dupName)
		replaceStrings = append(replaceStrings, canonName)
	}

	for _, asset := range assets {
		if asset.RelPath == dupRelPath || asset.RelPath == canonicalRelPath {
			continue
		}
		ext := strings.ToLower(filepath.Ext(asset.Path))
		if isBinaryFile(ext) {
			continue
		}

		content, err := os.ReadFile(asset.Path)
		if err != nil {
			continue
		}

		origContent := string(content)
		newContent := origContent
		replacedAny := false

		for i, searchStr := range searchStrings {
			if strings.Contains(newContent, searchStr) {
				newContent = strings.ReplaceAll(newContent, searchStr, replaceStrings[i])
				replacedAny = true
			}
		}

		if replacedAny {
			if err := backupFile(asset.RelPath); err != nil {
				fmt.Fprintf(os.Stderr, "Backup error for code file: %v\n", err)
				continue
			}

			err = os.WriteFile(asset.Path, []byte(newContent), 0644)
			if err == nil {
				*actions = append(*actions, core.ActionLog{
					Action:  "replace_reference",
					Path:    asset.RelPath,
					Target:  canonicalRelPath,
					Details: fmt.Sprintf("Replaced references of '%s' with '%s'", dupRelPath, canonicalRelPath),
				})
				fmt.Printf("✏️  Updated references in: %s\n", asset.RelPath)
			}
		}
	}
}

func isBinaryFile(ext string) bool {
	binaries := map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".ico":  true,
		".webp": true,
		".svg":  true,
		".pdf":  true,
		".zip":  true,
		".gz":   true,
		".tar":  true,
		".mp4":  true,
		".mp3":  true,
		".exe":  true,
		".dll":  true,
		".so":   true,
		".dylib":true,
		".woff": true,
		".woff2":true,
		".ttf":  true,
		".eot":  true,
		".o":    true,
		".a":    true,
		".framework": true,
		".app":  true,
		".nib":  true,
		".car":  true,
		".bin":  true,
		".db":   true,
		".sqlite": true,
		".ds_store": true,
	}
	return binaries[ext]
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

	if len(report.ActionsTaken) > 0 {
		fmt.Println("================================================================================")
		fmt.Println("🛠️  Actions Taken:")
		fmt.Println()
		for _, action := range report.ActionsTaken {
			fmt.Printf("   • %s: %s (%s)\n", strings.ToUpper(action.Action), action.Path, action.Details)
		}
		fmt.Println()
	}

	if len(report.ActionsSimulated) > 0 {
		fmt.Println("================================================================================")
		fmt.Println("🛠️  Simulated Actions (Dry-Run):")
		fmt.Println()
		for _, action := range report.ActionsSimulated {
			fmt.Printf("   • %s: %s (%s)\n", strings.ToUpper(action.Action), action.Path, action.Details)
		}
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
