package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanner(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "repotrim-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	files := map[string]string{
		"main.go":                  "package main",
		"core/models.go":           "package core",
		"assets/logo.png":          "fake png content",
		"node_modules/foo/index.js": "console.log('ignored')",
		"vendor/bar/main.go":       "package bar",
		".git/config":              "some git config",
		"custom_ignored/file.txt":  "ignored",
	}

	for rel, content := range files {
		fullPath := filepath.Join(tempDir, rel)
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		if err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		err = os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	scanner := NewScanner(ScannerConfig{
		RootDir:        tempDir,
		Workers:        2,
		IgnorePatterns: []string{"custom_ignored"},
	})

	assets, err := scanner.Scan()
	if err != nil {
		t.Fatalf("Scanner failed: %v", err)
	}

	// We expect: main.go, core/models.go, assets/logo.png
	// We expect node_modules, vendor, .git, and custom_ignored to be ignored.
	expectedFiles := map[string]bool{
		"main.go":         true,
		"core/models.go":  true,
		"assets/logo.png": true,
	}

	if len(assets) != len(expectedFiles) {
		t.Errorf("Expected %d assets, got %d", len(expectedFiles), len(assets))
	}

	for _, asset := range assets {
		if !expectedFiles[asset.RelPath] {
			t.Errorf("Unexpected scanned file: %s", asset.RelPath)
		}
	}
}

func TestParser(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "repotrim-parser-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	codeContent := `
		import "repotrim/core"
		const logo = "assets/logo.png";
		// A non-quoted path-like reference
		script = scripts/deploy.sh
	`
	codePath := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(codePath, []byte(codeContent), 0644); err != nil {
		t.Fatalf("failed to write test code file: %v", err)
	}

	assets := []Asset{
		{
			Path:      codePath,
			RelPath:   "main.go",
			Size:      int64(len(codeContent)),
			Extension: ".go",
		},
	}

	parser := NewParser(2)
	refs, err := parser.Parse(assets)
	if err != nil {
		t.Fatalf("Parser failed: %v", err)
	}

	// Verify we extracted "repotrim/core", "assets/logo.png", and "scripts/deploy.sh"
	expectedTokens := []string{"repotrim/core", "assets/logo.png", "scripts/deploy.sh"}
	for _, tok := range expectedTokens {
		if _, ok := refs[tok]; !ok {
			t.Errorf("Expected reference token '%s' not extracted", tok)
		}
	}
}

func TestAnalyzer(t *testing.T) {
	assets := []Asset{
		{
			Path:         "/project/main.go",
			RelPath:      "main.go",
			Size:         100,
			Extension:    ".go",
			SHA256:       "hash1",
			LastModified: time.Now(),
		},
		{
			Path:         "/project/assets/logo.png",
			RelPath:      "assets/logo.png",
			Size:         500,
			Extension:    ".png",
			SHA256:       "hash2",
			LastModified: time.Now(),
		},
		{
			Path:         "/project/assets/unused.png",
			RelPath:      "assets/unused.png",
			Size:         600,
			Extension:    ".png",
			SHA256:       "hash3",
			LastModified: time.Now(),
		},
		{
			Path:         "/project/assets/logo_copy.png", // duplicate of logo.png
			RelPath:      "assets/logo_copy.png",
			Size:         500,
			Extension:    ".png",
			SHA256:       "hash2", // Same SHA256
			LastModified: time.Now(),
		},
		{
			Path:         "/project/assets/large.mp4", // large media file >5MB
			RelPath:      "assets/large.mp4",
			Size:         6 * 1024 * 1024,
			Extension:    ".mp4",
			SHA256:       "hash4",
			LastModified: time.Now(),
		},
	}

	references := map[string][]Reference{
		"repotrim/core":    {{Token: "repotrim/core", SourceFile: "main.go"}},
		"assets/logo.png":  {{Token: "assets/logo.png", SourceFile: "main.go"}},
		"assets/large.mp4": {{Token: "assets/large.mp4", SourceFile: "main.go"}},
	}

	analyzer := NewAnalyzer(assets, references)
	report := analyzer.Analyze("/project", 10)

	// We expect issues:
	// 1. Unused asset: "assets/unused.png"
	// 2. Duplicate asset: "assets/logo_copy.png" (duplicate of "assets/logo.png")
	// 3. Large media: "assets/large.mp4" (warning only, savings is 0)
	// 4. Lfs tracking warning: "assets/large.mp4" (not tracked by Git LFS)

	if report.TotalIssuesFound != 4 {
		t.Errorf("Expected 4 issues, got %d", report.TotalIssuesFound)
	}

	var foundUnused, foundDup, foundLarge, foundLfs bool
	for _, issue := range report.Issues {
		switch issue.Type {
		case UnusedAsset:
			if issue.FilePath == "assets/unused.png" {
				foundUnused = true
				if issue.SavingsBytes != 600 {
					t.Errorf("Expected savings of 600 bytes for unused file, got %d", issue.SavingsBytes)
				}
			}
		case DuplicateAsset:
			if issue.FilePath == "assets/logo_copy.png" {
				foundDup = true
				if issue.SavingsBytes != 500 {
					t.Errorf("Expected savings of 500 bytes for duplicate file, got %d", issue.SavingsBytes)
				}
			}
		case LargeMedia:
			if issue.FilePath == "assets/large.mp4" {
				foundLarge = true
				if issue.SavingsBytes != 0 {
					t.Errorf("Expected savings of 0 bytes for large media warning, got %d", issue.SavingsBytes)
				}
			}
		case LfsTrackingWarning:
			if issue.FilePath == "assets/large.mp4" {
				foundLfs = true
			}
		}
	}

	if !foundUnused {
		t.Error("Missing expected UnusedAsset issue for assets/unused.png")
	}
	if !foundDup {
		t.Error("Missing expected DuplicateAsset issue for assets/logo_copy.png")
	}
	if !foundLarge {
		t.Error("Missing expected LargeMedia issue for assets/large.mp4")
	}
	if !foundLfs {
		t.Error("Missing expected LfsTrackingWarning issue for assets/large.mp4")
	}
}

func TestXcodeBoilerplateFilter(t *testing.T) {
	assets := []Asset{
		{
			Path:      "/project/App.xcassets/Contents.json",
			RelPath:   "App.xcassets/Contents.json",
			Size:      63,
			Extension: ".json",
			SHA256:    "hash-meta",
		},
		{
			Path:      "/project/App.xcassets/Onboarding/Contents.json",
			RelPath:   "App.xcassets/Onboarding/Contents.json",
			Size:      63,
			Extension: ".json",
			SHA256:    "hash-meta", // Same hash, duplicate!
		},
	}
	analyzer := NewAnalyzer(assets, nil)
	report := analyzer.Analyze("/project", 0)

	// Since they are inside .xcassets and named Contents.json, they must be ignored from duplicate detection!
	for _, issue := range report.Issues {
		if issue.Type == DuplicateAsset {
			t.Errorf("Boilerplate Contents.json inside .xcassets should be ignored for duplicates, but found issue: %v", issue)
		}
	}
}

func TestLfsMatching(t *testing.T) {
	patterns := []string{"*.png", "assets/**/*.mp4", "large_file.zip"}

	tests := []struct {
		path     string
		expected bool
	}{
		{"assets/logo.png", true},
		{"logo.png", true},
		{"assets/video.mp4", true},
		{"large_file.zip", true},
		{"assets/small.mp3", false},
	}

	for _, tc := range tests {
		matched := matchesLfsPattern(tc.path, patterns)
		if matched != tc.expected {
			t.Errorf("Expected path '%s' LFS match to be %t, got %t", tc.path, tc.expected, matched)
		}
	}
}

func TestEmptyDirectoryCleaner(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "repotrim-test-empty-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create empty folder
	emptySub := filepath.Join(tempDir, "empty_folder")
	if err := os.Mkdir(emptySub, 0755); err != nil {
		t.Fatalf("Failed to create subfolder: %v", err)
	}

	// Create non-empty folder
	nonEmptySub := filepath.Join(tempDir, "non_empty_folder")
	if err := os.Mkdir(nonEmptySub, 0755); err != nil {
		t.Fatalf("Failed to create subfolder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nonEmptySub, "file.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	analyzer := NewAnalyzer(nil, nil)
	issues := analyzer.findEmptyDirectories(tempDir)

	// We expect exactly 1 empty directory issue: "empty_folder"
	if len(issues) != 1 {
		t.Fatalf("Expected 1 empty directory issue, got %d", len(issues))
	}
	if issues[0].FilePath != "empty_folder" {
		t.Errorf("Expected empty directory to be 'empty_folder', got '%s'", issues[0].FilePath)
	}
}
