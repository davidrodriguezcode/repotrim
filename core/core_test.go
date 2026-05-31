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

	if report.TotalIssuesFound != 3 {
		t.Errorf("Expected 3 issues, got %d", report.TotalIssuesFound)
	}

	var foundUnused, foundDup, foundLarge bool
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
}
