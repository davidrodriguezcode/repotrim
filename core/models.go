package core

import "time"

// BloatIssueType categorizes the type of structural or asset bloat found.
type BloatIssueType string

const (
	UnusedAsset        BloatIssueType = "Unused Asset"
	DuplicateAsset     BloatIssueType = "Duplicate Asset"
	UnusedConfig       BloatIssueType = "Unused Configuration"
	LargeMedia         BloatIssueType = "Large Media Asset"
	EmptyDirectory     BloatIssueType = "Empty Directory"
	LfsTrackingWarning BloatIssueType = "Git LFS Tracking Warning"
	TrackedIgnoredFile BloatIssueType = "Tracked Ignored File"
)

// Asset represents a file scanned in the repository.
type Asset struct {
	Path         string    `json:"path"`          // Absolute file path
	RelPath      string    `json:"rel_path"`      // Relative path from scanning root
	Size         int64     `json:"size"`          // File size in bytes
	Extension    string    `json:"extension"`     // File extension (e.g. .png, .go)
	SHA256       string    `json:"sha256"`        // SHA256 checksum (computed for deduplication/validation)
	LastModified time.Time `json:"last_modified"` // Last modification timestamp
}

// Reference represents a symbolic or literal usage of an asset/file.
type Reference struct {
	Token      string `json:"token"`       // The string token (e.g., import name, path, variable content)
	SourceFile string `json:"source_file"` // File containing this reference
	LineNumber int    `json:"line_number"` // Line number of reference
}

// ScannerConfig represents configuration options for RepoTrim engine.
type ScannerConfig struct {
	RootDir        string   `json:"root_dir"`
	Workers        int      `json:"workers"`
	IgnorePatterns []string `json:"ignore_patterns"`
}

// BloatIssue describes a single optimization finding.
type BloatIssue struct {
	Type           BloatIssueType `json:"type"`
	FilePath       string         `json:"file_path"`       // Relative path to the file
	Details        string         `json:"details"`         // Description of why it's bloat
	SavingsBytes   int64          `json:"savings_bytes"`   // Potential space saved by deleting/fixing it
	Recommendation string         `json:"recommendation"`  // Suggested action (e.g., "Delete unused file", "Deduplicate with X")
}

// AnalysisReport compiles the metadata and results from an engine run.
type AnalysisReport struct {
	RootDir            string        `json:"root_dir"`
	TotalFilesScanned  int           `json:"total_files_scanned"`
	TotalBytesScanned  int64         `json:"total_bytes_scanned"`
	TotalIssuesFound   int           `json:"total_issues_found"`
	TotalSavingsBytes  int64         `json:"total_savings_bytes"`
	Issues             []BloatIssue  `json:"issues"`
	ExecutionTimeMs    int64         `json:"execution_time_ms"`

	// Validation fields required by spec
	InitialSizeBytes      int64    `json:"initial_size_bytes"`
	ProjectedSavingsBytes int64    `json:"projected_savings_bytes"`
	UnusedAssets          []string `json:"unused_assets"`
	ProtectedAssets       []string `json:"protected_assets"`

	// Execution actions taken/simulated
	ActionsTaken     []ActionLog `json:"actions_taken,omitempty"`
	ActionsSimulated []ActionLog `json:"actions_simulated,omitempty"`
}

// ActionLog records a pruning or modification action taken by RepoTrim.
type ActionLog struct {
	Action  string `json:"action"`  // e.g. "delete", "replace_reference", "remove_dir"
	Path    string `json:"path"`    // File/dir path acted upon
	Target  string `json:"target"`  // e.g. replacement string or value, if applicable
	Details string `json:"details"` // Explanation of action
}
