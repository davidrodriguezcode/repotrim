package core

import (
	"fmt"
	"path/filepath"
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
