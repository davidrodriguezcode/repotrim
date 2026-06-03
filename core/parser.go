package core

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"sync"
	"text/scanner"
)

// Parser handles token extraction from files.
type Parser struct {
	workers int
}

// NewParser initializes a Parser with concurrency limits.
func NewParser(workers int) *Parser {
	if workers <= 0 {
		workers = 4
	}
	return &Parser{workers: workers}
}

// Parse extracts reference tokens concurrently from code/config files and stores them.
func (p *Parser) Parse(assets []Asset) (map[string][]Reference, error) {
	assetsChan := make(chan Asset, len(assets))
	for _, asset := range assets {
		assetsChan <- asset
	}
	close(assetsChan)

	var wg sync.WaitGroup
	var mu sync.Mutex
	references := make(map[string][]Reference)

	// String literal regular expression (captures text inside double quotes, single quotes, backticks)
	strRegex := regexp.MustCompile(`["'` + "`" + `]([^"'` + "`" + `\n\r\t]+)["'` + "`" + `]`)
	// Basic regex for potential file/path reference in yaml, scripts, or non-quoted files (e.g., scripts/build.sh)
	pathRegex := regexp.MustCompile(`(?i)[a-z0-9_\-\./]+\.[a-z0-9_]{2,8}`)

	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for asset := range assetsChan {
				if p.isBinary(asset.Extension) {
					continue
				}

				var fileRefs map[string][]Reference
				var err error

				// Optimization: Use both highly efficient text/scanner (as planned) and generic regex parser for Go files
				if asset.Extension == ".go" {
					fileRefs, err = p.parseGoFile(asset)
					genericRefs, errGen := p.parseGenericFile(asset, strRegex, pathRegex)
					if errGen == nil {
						if fileRefs == nil {
							fileRefs = make(map[string][]Reference)
						}
						// Merge, avoiding duplicates
						for token, refs := range genericRefs {
							if _, exists := fileRefs[token]; !exists {
								fileRefs[token] = refs
							}
						}
					}
				} else {
					fileRefs, err = p.parseGenericFile(asset, strRegex, pathRegex)
				}

				if err == nil && len(fileRefs) > 0 {
					mu.Lock()
					for token, refs := range fileRefs {
						references[token] = append(references[token], refs...)
					}
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	return references, nil
}

// parseGoFile uses text/scanner to extract raw string literals with zero heavy runtime overhead.
func (p *Parser) parseGoFile(asset Asset) (map[string][]Reference, error) {
	file, err := os.Open(asset.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	localRefs := make(map[string][]Reference)
	var s scanner.Scanner
	s.Init(file)
	s.Filename = asset.Path

	// Avoid scan errors stopping tokenization (e.g. malformed comments)
	s.Error = func(s *scanner.Scanner, msg string) {}

	// Scan through code tokens looking specifically for string components
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		if tok == scanner.String {
			// Clean raw quotes off the token (e.g. `"icon_home"` -> `icon_home`)
			cleanToken := strings.Trim(s.TokenText(), "\"")
			cleanToken = strings.Trim(cleanToken, "`")
			cleanToken = strings.TrimSpace(cleanToken)

			if len(cleanToken) > 0 {
				lineNum := s.Position.Line
				localRefs[cleanToken] = append(localRefs[cleanToken], Reference{
					Token:      cleanToken,
					SourceFile: asset.RelPath,
					LineNumber: lineNum,
				})
			}
		}
	}

	return localRefs, nil
}

// parseGenericFile parses a single file line by line to extract references using regex.
func (p *Parser) parseGenericFile(asset Asset, strRegex, pathRegex *regexp.Regexp) (map[string][]Reference, error) {
	f, err := os.Open(asset.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	localRefs := make(map[string][]Reference)
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Extract string quotes
		matches := strRegex.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			if len(m) > 1 {
				token := strings.TrimSpace(m[1])
				if token != "" {
					localRefs[token] = append(localRefs[token], Reference{
						Token:      token,
						SourceFile: asset.RelPath,
						LineNumber: lineNum,
					})
				}
			}
		}

		// Extract raw path patterns (useful for YAML configs, scripts, etc. where quotes aren't always used)
		pathMatches := pathRegex.FindAllString(line, -1)
		for _, rawToken := range pathMatches {
			token := strings.TrimSpace(rawToken)
			if token != "" {
				localRefs[token] = append(localRefs[token], Reference{
					Token:      token,
					SourceFile: asset.RelPath,
					LineNumber: lineNum,
				})
			}
		}
	}

	return localRefs, nil
}

// isBinary checks if the file suffix matches standard binary extensions.
func (p *Parser) isBinary(ext string) bool {
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
