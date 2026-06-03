package core

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Analyzer implements the rules engine.
type Analyzer struct {
	assets     []Asset
	references map[string][]Reference
}

// NewAnalyzer creates a new rules analyzer.
func NewAnalyzer(assets []Asset, references map[string][]Reference) *Analyzer {
	return &Analyzer{
		assets:     assets,
		references: references,
	}
}

// Analyze evaluates all rules and returns an AnalysisReport.
func (a *Analyzer) Analyze(rootDir string, durationMs int64) AnalysisReport {
	var issues []BloatIssue
	var totalBytes int64

	// Gather metadata
	for _, asset := range a.assets {
		totalBytes += asset.Size
	}

	// 1. Analyze Duplicates (Group by SHA256)
	dupIssues, duplicatedPaths := a.analyzeDuplicates()
	issues = append(issues, dupIssues...)

	// 2. Analyze Dead Assets (skipping duplicates)
	deadIssues := a.analyzeDeadAssets(duplicatedPaths)
	issues = append(issues, deadIssues...)

	// 3. Analyze Large Assets (skipping duplicates and unused assets)
	largeIssues := a.analyzeLargeAssets(duplicatedPaths)
	issues = append(issues, largeIssues...)

	// 4. Analyze Empty Directories
	emptyDirIssues := a.findEmptyDirectories(rootDir)
	issues = append(issues, emptyDirIssues...)

	// 5. Analyze Git LFS configuration
	lfsIssues := a.analyzeLfsTracking(rootDir)
	issues = append(issues, lfsIssues...)

	// 6. Analyze Tracked Ignored Files
	ignoredIssues := a.analyzeTrackedIgnored(rootDir)
	issues = append(issues, ignoredIssues...)

	// Calculate total savings
	var totalSavings int64
	for _, issue := range issues {
		totalSavings += issue.SavingsBytes
	}

	// Sort issues deterministically by file path and then by type
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].FilePath == issues[j].FilePath {
			return issues[i].Type < issues[j].Type
		}
		return issues[i].FilePath < issues[j].FilePath
	})

	// Calculate spec-required validation fields
	isAssetExt := map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".ico":  true,
		".webp": true,
		".svg":  true,
		".mp4":  true,
		".mp3":  true,
		".pdf":  true,
	}

	var initialSizeBytes int64
	var projectedSavingsBytes int64
	var unusedAssets []string
	var protectedAssets []string

	for _, asset := range a.assets {
		if isAssetExt[asset.Extension] {
			initialSizeBytes += asset.Size

			if a.isAlwaysActive(asset) {
				continue
			}

			used, isPrefix := a.checkReference(asset)
			if !used {
				unusedAssets = append(unusedAssets, asset.RelPath)
				projectedSavingsBytes += asset.Size
			} else if isPrefix {
				protectedAssets = append(protectedAssets, asset.RelPath)
			}
		}
	}

	// Always return initialized slices rather than nil
	if unusedAssets == nil {
		unusedAssets = []string{}
	}
	if protectedAssets == nil {
		protectedAssets = []string{}
	}

	return AnalysisReport{
		RootDir:               rootDir,
		TotalFilesScanned:     len(a.assets),
		TotalBytesScanned:     totalBytes,
		TotalIssuesFound:      len(issues),
		TotalSavingsBytes:     totalSavings,
		Issues:                issues,
		ExecutionTimeMs:       durationMs,
		InitialSizeBytes:      initialSizeBytes,
		ProjectedSavingsBytes: projectedSavingsBytes,
		UnusedAssets:          unusedAssets,
		ProtectedAssets:       protectedAssets,
	}
}

// analyzeDuplicates groups assets by hash to spot exact copies.
func (a *Analyzer) analyzeDuplicates() ([]BloatIssue, map[string]bool) {
	var issues []BloatIssue
	duplicatedPaths := make(map[string]bool)
	hashMap := make(map[string][]Asset)

	for _, asset := range a.assets {
		if asset.SHA256 != "" && asset.Size > 0 {
			// Skip boilerplate Contents.json files inside .xcassets folders (standard Xcode metadata)
			relLower := strings.ToLower(filepath.ToSlash(asset.RelPath))
			if strings.Contains(relLower, ".xcassets/") && strings.HasSuffix(relLower, "contents.json") {
				continue
			}
			hashMap[asset.SHA256] = append(hashMap[asset.SHA256], asset)
		}
	}

	for _, group := range hashMap {
		if len(group) > 1 {
			// Sort group by relative path length to keep the shortest / most canonical one
			sort.Slice(group, func(i, j int) bool {
				return len(group[i].RelPath) < len(group[j].RelPath)
			})

			canonical := group[0]
			for i := 1; i < len(group); i++ {
				duplicate := group[i]
				duplicatedPaths[duplicate.RelPath] = true
				issues = append(issues, BloatIssue{
					Type:         DuplicateAsset,
					FilePath:     duplicate.RelPath,
					Details:      fmt.Sprintf("Duplicate of '%s' (identical SHA256)", canonical.RelPath),
					SavingsBytes: duplicate.Size,
					Recommendation: fmt.Sprintf("Delete duplicate file and update references to point to '%s'", canonical.RelPath),
				})
			}
		}
	}
	return issues, duplicatedPaths
}

// analyzeDeadAssets finds assets not referenced anywhere.
func (a *Analyzer) analyzeDeadAssets(duplicatedPaths map[string]bool) []BloatIssue {
	var issues []BloatIssue

	for _, asset := range a.assets {
		if a.isAlwaysActive(asset) {
			continue
		}
		if duplicatedPaths[asset.RelPath] {
			continue
		}

		if !a.isReferenced(asset) {
			issueType := UnusedAsset
			rec := "Delete unused asset file"

			// Check if it's a config file
			if asset.Extension == ".json" || asset.Extension == ".yml" || asset.Extension == ".yaml" || asset.Extension == ".conf" {
				issueType = UnusedConfig
				rec = "Remove unused configuration file"
			}

			issues = append(issues, BloatIssue{
				Type:           issueType,
				FilePath:       asset.RelPath,
				Details:        "No references or imports found in source files or configuration files",
				SavingsBytes:   asset.Size,
				Recommendation: rec,
			})
		}
	}
	return issues
}

// analyzeLargeAssets flags excessively large files.
func (a *Analyzer) analyzeLargeAssets(duplicatedPaths map[string]bool) []BloatIssue {
	var issues []BloatIssue
	// Threshold = 5MB
	const threshold = 5 * 1024 * 1024

	mediaExts := map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".mp4":  true,
		".mp3":  true,
		".zip":  true,
		".gz":   true,
		".tar":  true,
		".pdf":  true,
	}

	for _, asset := range a.assets {
		if duplicatedPaths[asset.RelPath] {
			continue
		}

		// Only check large assets if they are actually active/referenced/whitelisted.
		// Unused ones are already flagged as UnusedAsset where they'll be recommended for deletion.
		if asset.Size > threshold && mediaExts[asset.Extension] {
			if a.isAlwaysActive(asset) || a.isReferenced(asset) {
				issues = append(issues, BloatIssue{
					Type:           LargeMedia,
					FilePath:       asset.RelPath,
					Details:        fmt.Sprintf("File size is %.2f MB, which exceeds the optimization threshold (5.00 MB)", float64(asset.Size)/(1024*1024)),
					SavingsBytes:   0, // Not safe to compute savings since it might be needed, just a warning
					Recommendation: "Compress this media asset or store it in an external object storage system rather than in repository git history",
				})
			}
		}
	}
	return issues
}

// isAlwaysActive checks if the asset is an entry point or standard config.
func (a *Analyzer) isAlwaysActive(asset Asset) bool {
	// Lowercase paths
	relLower := strings.ToLower(filepath.ToSlash(asset.RelPath))
	base := strings.ToLower(filepath.Base(asset.Path))

	// Main entries
	if base == "main.go" || strings.HasPrefix(base, "main_") || base == "app.go" || base == "index.js" || base == "index.ts" {
		return true
	}

	// standard docs, files, configurations at root
	if !strings.Contains(relLower, "/") {
		if base == "go.mod" || base == "go.sum" || base == "package.json" || base == "package-lock.json" ||
			base == "yarn.lock" || base == "pnpm-lock.yaml" || base == "readme.md" || base == "license" ||
			base == "makefile" || base == "dockerfile" || base == "docker-compose.yml" || base == ".gitignore" {
			return true
		}
	}

	// CI/CD workflows under .github
	if strings.HasPrefix(relLower, ".github/workflows/") {
		return true
	}

	// Go test files are active
	if strings.HasSuffix(base, "_test.go") {
		return true
	}

	return false
}

// isReferenced checks if the file has any matches in the extracted references map.
func (a *Analyzer) isReferenced(asset Asset) bool {
	used, _ := a.checkReference(asset)
	return used
}

// checkReference checks references for a file and returns if it is used, and if it's purely a prefix match.
func (a *Analyzer) checkReference(asset Asset) (bool, bool) {
	base := filepath.Base(asset.Path)
	ext := strings.ToLower(filepath.Ext(asset.Path))
	baseWithoutExt := strings.TrimSuffix(base, ext)
	relPathSlash := filepath.ToSlash(asset.RelPath)

	isMatched := false
	isExact := false
	isPrefix := false

	for token := range a.references {
		cleanToken := strings.TrimPrefix(token, "./")
		cleanToken = strings.TrimPrefix(cleanToken, "../")
		cleanToken = strings.TrimPrefix(cleanToken, "/")

		// Direct exact match of token to rel path or basename with/without extension
		if cleanToken == relPathSlash || cleanToken == base || cleanToken == baseWithoutExt {
			isMatched = true
			isExact = true
		}

		// Suffix match
		if strings.HasSuffix(relPathSlash, cleanToken) {
			isMatched = true
			isExact = true
		}

		// Prefix match on base name (e.g. token "icon_" matches "icon_home")
		if cleanToken != "" && strings.HasPrefix(baseWithoutExt, cleanToken) {
			isMatched = true
			if cleanToken != baseWithoutExt {
				isPrefix = true
			} else {
				isExact = true
			}
		}

		// Fallback partial match
		if strings.Contains(relPathSlash, cleanToken) {
			isMatched = true
		}
	}

	if isMatched {
		if isExact {
			return true, false
		}
		if isPrefix {
			return true, true
		}
		return true, false
	}

	return false, false
}

// findEmptyDirectories walks the target rootDir and flags empty directories.
func (a *Analyzer) findEmptyDirectories(rootDir string) []BloatIssue {
	var issues []BloatIssue

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil || relPath == "." || relPath == "" {
			return nil
		}

		// Skip hidden folders like .git, and common dependency dirs
		relPathLower := strings.ToLower(filepath.ToSlash(relPath))
		if strings.HasPrefix(relPathLower, ".git") || strings.Contains(relPathLower, "/.git") ||
			strings.Contains(relPathLower, "node_modules") || strings.Contains(relPathLower, "vendor") {
			return nil
		}

		empty, err := isDirEmpty(path)
		if err == nil && empty {
			issues = append(issues, BloatIssue{
				Type:           EmptyDirectory,
				FilePath:       relPath,
				Details:        "This directory contains no files or subdirectories",
				SavingsBytes:   0,
				Recommendation: "Delete this empty directory to maintain workspace cleanliness",
			})
		}
		return nil
	})

	if err != nil {
		return nil
	}
	return issues
}

// isDirEmpty checks recursively if a directory contains any files or active subfolders.
func isDirEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		return false, err
	}

	if len(names) == 0 {
		return true, nil
	}

	for _, n := range names {
		// Ignore hidden files like .DS_Store from preventing a directory from being empty
		if n == ".DS_Store" {
			continue
		}
		childPath := filepath.Join(name, n)
		childInfo, err := os.Stat(childPath)
		if err != nil {
			continue
		}
		if !childInfo.IsDir() {
			return false, nil
		}
		empty, err := isDirEmpty(childPath)
		if err != nil || !empty {
			return false, nil
		}
	}

	return true, nil
}

// parseGitAttributes parses patterns from a local .gitattributes file.
func parseGitAttributes(rootDir string) ([]string, error) {
	path := filepath.Join(rootDir, ".gitattributes")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "filter=lfs") || strings.Contains(line, "merge=lfs") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				patterns = append(patterns, parts[0])
			}
		}
	}
	return patterns, nil
}

// matchesLfsPattern determines if a path satisfies a Git LFS attribute.
func matchesLfsPattern(relPath string, patterns []string) bool {
	relPathNorm := filepath.ToSlash(relPath)
	base := filepath.Base(relPath)

	for _, pattern := range patterns {
		patternNorm := filepath.ToSlash(pattern)
		
		// If pattern doesn't contain a slash, it matches the base name of the file
		if !strings.Contains(patternNorm, "/") {
			if matchGitPattern(patternNorm, base) {
				return true
			}
		} else {
			// Otherwise it matches the relative path
			patternNorm = strings.TrimPrefix(patternNorm, "/")
			if matchGitPattern(patternNorm, relPathNorm) {
				return true
			}
		}
	}
	return false
}

// matchGitPattern handles standard Git glob wildcard translations.
func matchGitPattern(pattern, path string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	if strings.HasPrefix(pattern, "*.") {
		ext := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(path, ext)
	}

	reStr := "^"
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '.':
			reStr += "\\."
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				reStr += ".*"
				i++
				// Skip trailing slash if "**/"
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
				}
			} else {
				reStr += "[^/]*"
			}
		case '?':
			reStr += "[^/]"
		case '/':
			reStr += "/"
		default:
			reStr += string(c)
		}
	}
	reStr += "$"

	re, err := regexp.Compile(reStr)
	if err != nil {
		return strings.Contains(path, pattern)
	}
	return re.MatchString(path)
}

// analyzeLfsTracking flags oversized files not tracked via Git LFS.
func (a *Analyzer) analyzeLfsTracking(rootDir string) []BloatIssue {
	var issues []BloatIssue
	patterns, _ := parseGitAttributes(rootDir)

	const threshold = 5 * 1024 * 1024
	mediaExts := map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".mp4":  true,
		".mp3":  true,
		".zip":  true,
		".gz":   true,
		".tar":  true,
		".pdf":  true,
	}

	for _, asset := range a.assets {
		if asset.Size > threshold && mediaExts[asset.Extension] {
			if !matchesLfsPattern(asset.RelPath, patterns) {
				issues = append(issues, BloatIssue{
					Type:           LfsTrackingWarning,
					FilePath:       asset.RelPath,
					Details:        fmt.Sprintf("Large media asset (%.2f MB) is not tracked by Git LFS in .gitattributes", float64(asset.Size)/(1024*1024)),
					SavingsBytes:   0,
					Recommendation: fmt.Sprintf("Add '%s filter=lfs diff=lfs merge=lfs -text' to your .gitattributes file", asset.Extension),
				})
			}
		}
	}
	return issues
}

// analyzeTrackedIgnored finds files listed in gitignore but currently checked into Git history.
func (a *Analyzer) analyzeTrackedIgnored(rootDir string) []BloatIssue {
	var issues []BloatIssue

	gitCheck := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	gitCheck.Dir = rootDir
	if err := gitCheck.Run(); err != nil {
		return nil
	}

	cmd := exec.Command("git", "ls-files", "-i", "--exclude-standard")
	cmd.Dir = rootDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		relPath := strings.TrimSpace(line)
		if relPath == "" {
			continue
		}

		var size int64
		for _, asset := range a.assets {
			if filepath.ToSlash(asset.RelPath) == filepath.ToSlash(relPath) {
				size = asset.Size
				break
			}
		}

		issues = append(issues, BloatIssue{
			Type:           TrackedIgnoredFile,
			FilePath:       relPath,
			Details:        "This file is listed in .gitignore but is currently tracked in the Git repository",
			SavingsBytes:   size,
			Recommendation: fmt.Sprintf("Remove from git tracking using: git rm --cached %s", relPath),
		})
	}

	return issues
}
