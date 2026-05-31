package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	demoDir := "test_workspace"
	fmt.Printf("Setting up mock test workspace: %s\n", demoDir)

	files := map[string]string{
		"main.go": `package main
import "fmt"
func main() {
	fmt.Println("Demo App starting...")
	const image = "assets/active_image.png"
	const script = "scripts/build.sh"
	const config = "config/app.yml"
	const media = "assets/oversized.mp4"
	fmt.Printf("Referencing active files: %s, %s, %s, %s\n", image, script, config, media)
}`,
		"assets/active_image.png":     "active-image-content-12345",
		"assets/unused_image.png":     "completely-different-unused-content",
		"assets/duplicate_image.png":  "active-image-content-12345", // duplicate of active_image.png
		"scripts/build.sh":            "#!/bin/bash\necho 'Building...'",
		"scripts/unused_script.sh":    "#!/bin/bash\necho 'Unused!'",
		"config/app.yml":              "port: 8080\nenv: production",
		"config/unused.yml":            "debug: true",
	}

	for rel, content := range files {
		full := filepath.Join(demoDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			fmt.Printf("Error creating directory: %v\n", err)
			return
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			fmt.Printf("Error writing file %s: %v\n", rel, err)
			return
		}
	}

	// Create oversized file (> 5MB)
	largePath := filepath.Join(demoDir, "assets/oversized.mp4")
	largeFile, err := os.Create(largePath)
	if err != nil {
		fmt.Printf("Error creating oversized file: %v\n", err)
		return
	}
	defer largeFile.Close()

	// Write 6 MB of empty bytes
	buf := make([]byte, 1024*1024) // 1MB buffer
	for i := 0; i < 6; i++ {
		if _, err := largeFile.Write(buf); err != nil {
			fmt.Printf("Error writing oversized content: %v\n", err)
			return
		}
	}

	fmt.Println("Mock workspace setup completed successfully!")
}
