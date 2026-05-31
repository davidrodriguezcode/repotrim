package core

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Scanner encapsulates the scan logic.
type Scanner struct {
	config ScannerConfig
}

// NewScanner initializes a new Scanner.
func NewScanner(config ScannerConfig) *Scanner {
	if config.Workers <= 0 {
		config.Workers = 4 // Default to 4 workers
	}
	return &Scanner{config: config}
}

// Scan crawls the target directory concurrently and returns the list of assets.
func (s *Scanner) Scan() ([]Asset, error) {
	pathsChan := make(chan string, 500)
	assetsChan := make(chan Asset, 500)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < s.config.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range pathsChan {
				asset, err := s.processFile(path)
				if err == nil {
					assetsChan <- asset
				}
			}
		}()
	}

	// Channel to gather completed assets
	var assets []Asset
	doneChan := make(chan struct{})
	go func() {
		for asset := range assetsChan {
			assets = append(assets, asset)
		}
		close(doneChan)
	}()

	// Walk directory and queue files
	err := filepath.Walk(s.config.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(s.config.RootDir, path)
		if err != nil {
			return nil
		}

		if s.isIgnored(relPath, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !info.IsDir() {
			pathsChan <- path
		}
		return nil
	})

	close(pathsChan)
	wg.Wait()
	close(assetsChan)
	<-doneChan

	return assets, err
}

// processFile reads file metadata and computes SHA256 checksum.
func (s *Scanner) processFile(path string) (Asset, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Asset{}, err
	}

	relPath, err := filepath.Rel(s.config.RootDir, path)
	if err != nil {
		relPath = path
	}

	// Compute SHA256
	hashStr, err := s.computeSHA256(path)
	if err != nil {
		hashStr = ""
	}

	return Asset{
		Path:         path,
		RelPath:      relPath,
		Size:         info.Size(),
		Extension:    strings.ToLower(filepath.Ext(path)),
		SHA256:       hashStr,
		LastModified: info.ModTime(),
	}, nil
}

// computeSHA256 calculates the SHA256 hash of a file.
func (s *Scanner) computeSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isIgnored checks if a path matches the configured ignore patterns.
func (s *Scanner) isIgnored(relPath string, isDir bool) bool {
	if relPath == "." || relPath == "" {
		return false
	}

	// Normalize windows path separators if any
	relPathNorm := filepath.ToSlash(relPath)
	parts := strings.Split(relPathNorm, "/")

	// Core default ignore list
	defaults := []string{".git", "node_modules", "vendor", ".DS_Store", "bin", "obj"}
	allPatterns := append(defaults, s.config.IgnorePatterns...)

	for _, part := range parts {
		for _, pattern := range allPatterns {
			// Exact match for path components
			if part == pattern {
				return true
			}
			// Glob/wildcard match
			matched, err := filepath.Match(pattern, part)
			if err == nil && matched {
				return true
			}
		}
	}

	// Full path matching (glob/wildcards)
	for _, pattern := range allPatterns {
		matched, err := filepath.Match(pattern, relPathNorm)
		if err == nil && matched {
			return true
		}
		// Suffix match
		if strings.HasSuffix(relPathNorm, "/"+pattern) {
			return true
		}
	}

	return false
}
