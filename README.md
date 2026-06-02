# 📂 RepoTrim Core Engine (Go)

RepoTrim is a high-performance, vendor-agnostic repository optimization engine written in Go. Designed to run concurrently and deterministically, RepoTrim walks your local workspace, parses abstract syntax tree (AST) structures, and tokenizes strings to identify **dead assets**, **duplicate files**, and **structural bloat** in CI/CD pipelines.

---

## ✨ Features

- **⚡ Concurrent Scanner**: Walks directories utilizing Go worker pools and computes SHA256 file hashes to detect identical duplicate assets.
- **🔍 Hybrid Token Extractor**: Uses standard library `text/scanner` to parse Go source files with zero runtime overhead, supplemented by a robust regular expression engine for JS, TS, Swift, Dart, YAML, JSON, CSS, and HTML files.
- **🧠 Heuristics Rules Engine**: Resolves file paths against active tokens. Safely flags unreferenced assets/configs while respecting an entry-point whitelist (e.g. `main.go`, `go.mod`, GitHub Actions workflows).
- **🔒 Licensing Layer**: Restricts execution to valid keys via the `REPOTRIM_LICENSE_KEY` environment variable. Outbound validations use a strict 3-second timeout and fail gracefully if networks are unreachable.
- **📦 Multi-Arch Distribution**: Cross-compilation settings utilizing GoReleaser for Linux, macOS, and Windows on both AMD64 and ARM64.

---

## 📁 Workspace Structure

```text
repotrim/
├── core/
│   ├── models.go       # Agnostic data contracts & structs
│   ├── scanner.go      # Concurrent filesystem asset crawler
│   ├── parser.go       # Concurrent tokenizer & AST token extractor
│   ├── analyzer.go     # Heuristic rules and prefix analysis engine
│   ├── licensing.go    # Outbound license validator & HTTP client
│   └── core_test.go    # Complete suite of unit tests
├── demo/
│   └── setup_demo.go   # Script to generate mock workspaces with bloat
├── main.go             # Entrypoint bootstrap, flags parser & CLI
├── go.mod              # Go module definition
├── install.sh          # Universal POSIX installation bootstrap
└── .goreleaser.yaml    # Cross-compilation release schema
```

---

## 🚀 Quick Start

### 1. Requirements
Ensure you have [Go](https://go.dev/) (version 1.20+) installed on your local host system.

### 2. Clone and Setup
Run the mock verification generator to automatically build a test environment with duplicates, dead files, and large media:
```bash
go run demo/setup_demo.go
```

### 3. Build & Compile
Verify the project compiles cleanly:
```bash
go build -v -o repotrim main.go
```

### 4. Running the Engine
RepoTrim requires the `REPOTRIM_LICENSE_KEY` environment variable to execute. For offline validation or testing, you can use our built-in offline developer bypass keys:

*   **Standard Human-Readable Terminal Format:**
    ```bash
    REPOTRIM_LICENSE_KEY=TEST_LICENSE_KEY ./repotrim -dir test_workspace
    ```

*   **Structured JSON Payload Format (for CI/CD pipeline automation):**
    ```bash
    REPOTRIM_LICENSE_KEY=TEST_LICENSE_KEY ./repotrim -dir test_workspace -format json
    ```

---

## 🔒 License Verification

The entry point enforces licensing gates via `VerifyLicense` inside `core/licensing.go`:
- Outbound verification hits `https://api.repotrim.innsoftlabs.com/v1/verify` using a `POST` request carrying the bearer token.
- Outbound network requests timeout cleanly after `3 seconds` to prevent pipelines from stalling.
- Network lookups or unreachable unroutable states resolve gracefully with standard, human-readable terminal alerts rather than panicking or crashing.

To run offline in local sandboxes or unit test environments, export the developer bypass key:
```bash
export REPOTRIM_LICENSE_KEY="TEST_LICENSE_KEY"
```

---

## 📦 Distribution & Installation

### Compilation Architecture
GoReleaser cross-compiles static binaries targeting major platforms:
- **OS**: `linux`, `darwin` (macOS), `windows`
- **Architectures**: `amd64`, `arm64`
- **Variables**: `CGO_ENABLED=0` static links.

To preview release archives locally, install GoReleaser and run:
```bash
goreleaser release --snapshot --clean
```

### Universal Installer
The repository includes a POSIX-compliant installer script (`install.sh`) that dynamically reads your local environment architecture and maps target downloads matching GoReleaser conventions:
```bash
chmod +x install.sh
./install.sh
```

---

## 🧪 Unit Testing

RepoTrim contains table-driven unit tests validating ignore rules, parsing strategies, duplicate comparisons, and license validation states:
```bash
go test -v ./core/...
```
