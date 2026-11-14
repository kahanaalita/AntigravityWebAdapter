package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundler(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bundler-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	_ = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("# Test Project\n```bash\necho hello\n```\n"), 0644)

	// Create ignored directory
	gitDir := filepath.Join(tempDir, ".git")
	_ = os.Mkdir(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0644)

	nodeDir := filepath.Join(tempDir, "node_modules")
	_ = os.Mkdir(nodeDir, 0755)
	_ = os.WriteFile(filepath.Join(nodeDir, "foo.js"), []byte("console.log('ignored');\n"), 0644)

	// Create binary file
	binData := []byte{0x00, 0x01, 0x02, 0xFF, 0x00}
	_ = os.WriteFile(filepath.Join(tempDir, "app.bin"), binData, 0644)

	b := New(Config{RootDir: tempDir})
	doc, stats, err := b.Bundle()
	if err != nil {
		t.Fatalf("Bundle failed: %v", err)
	}

	if stats.TotalFiles != 2 {
		t.Errorf("Expected 2 included files, got %d", stats.TotalFiles)
	}

	if !strings.Contains(doc, "main.go") || !strings.Contains(doc, "README.md") {
		t.Errorf("Document missing main.go or README.md")
	}

	if !strings.Contains(doc, "````md\n# Test Project") {
		t.Errorf("Document should use 4 backticks for README.md containing backticks")
	}

	if strings.Contains(doc, "foo.js") || strings.Contains(doc, "app.bin") {
		t.Errorf("Document contains ignored or binary files")
	}

	if stats.EstimatedTokens <= 0 {
		t.Errorf("EstimatedTokens must be positive, got %d", stats.EstimatedTokens)
	}
}
