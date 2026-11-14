package bundler

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DefaultIgnores contains directories and files ignored by default.
var DefaultIgnores = []string{
	".git",
	".DS_Store",
	"node_modules",
	"vendor",
	"bin",
	"dist",
	"build",
	".idea",
	".vscode",
	".next",
	".turbo",
	"coverage",
	"profile",
	"*.log",
	"*.tar",
	"*.gz",
	"*.zip",
	"*.7z",
	"*.png",
	"*.jpg",
	"*.jpeg",
	"*.webp",
	"*.gif",
	"*.ico",
	"*.pdf",
	"*.dylib",
	"*.so",
	"*.dll",
	"*.exe",
	"*.a",
	"*.o",
}

// Stats holds information about the bundled codebase.
type Stats struct {
	TotalFiles      int
	TotalBytes      int64
	EstimatedTokens int
	FileTree        string
}

// Config configures the bundling process.
type Config struct {
	RootDir        string
	IgnorePatterns []string
	MaxFileSize    int64 // default 1MB per single file
}

// Bundler traverses a directory and produces a single structured markdown document.
type Bundler struct {
	cfg Config
}

// New creates a new Bundler with default configuration.
func New(cfg Config) *Bundler {
	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = 1024 * 1024 // 1 MB limit per file
	}
	return &Bundler{cfg: cfg}
}

// Bundle traverses the root directory and returns the markdown document and stats.
func (b *Bundler) Bundle() (string, *Stats, error) {
	root, err := filepath.Abs(b.cfg.RootDir)
	if err != nil {
		return "", nil, fmt.Errorf("invalid root path: %w", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return "", nil, fmt.Errorf("failed to stat root directory: %w", err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("root path is not a directory: %s", root)
	}

	ignores := b.loadIgnores(root)

	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}

		if b.isIgnored(rel, d.IsDir(), ignores) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.IsDir() {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("walk directory failed: %w", err)
	}

	// Filter out unreadable or binary files
	var textFiles []string
	fileContents := make(map[string][]byte)

	for _, rel := range files {
		fullPath := filepath.Join(root, rel)
		finfo, err := os.Stat(fullPath)
		if err != nil || finfo.Size() > b.cfg.MaxFileSize {
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil || isBinary(data) {
			continue
		}

		textFiles = append(textFiles, rel)
		fileContents[rel] = data
	}

	// Generate tree for text files
	tree := b.buildTree(textFiles)

	var doc bytes.Buffer
	projectName := filepath.Base(root)

	doc.WriteString(fmt.Sprintf("# Codebase Dump: %s\n\n", projectName))
	doc.WriteString(fmt.Sprintf("- **Root Directory**: `%s`\n", root))
	doc.WriteString(fmt.Sprintf("- **Total Files Included**: %d\n\n", len(textFiles)))

	doc.WriteString("## Project File Structure\n\n```text\n")
	doc.WriteString(tree)
	doc.WriteString("```\n\n")

	doc.WriteString("## File Contents\n\n")

	totalBytes := int64(0)
	includedCount := 0

	for _, rel := range textFiles {
		data := fileContents[rel]
		includedCount++
		totalBytes += int64(len(data))
		ext := strings.TrimPrefix(filepath.Ext(rel), ".")
		if ext == "" {
			ext = "text"
		}

		fence := "```"
		if bytes.Contains(data, []byte("```")) {
			fence = "````"
			if bytes.Contains(data, []byte("````")) {
				fence = "`````"
			}
		}

		doc.WriteString(fmt.Sprintf("### `%s`\n\n%s%s\n", rel, fence, ext))
		doc.Write(data)
		if !bytes.HasSuffix(data, []byte("\n")) {
			doc.WriteString("\n")
		}
		doc.WriteString(fmt.Sprintf("%s\n\n", fence))
	}

	output := doc.String()
	stats := &Stats{
		TotalFiles:      includedCount,
		TotalBytes:      totalBytes,
		EstimatedTokens: len(output) / 4,
		FileTree:        tree,
	}

	return output, stats, nil
}

func (b *Bundler) loadIgnores(root string) []string {
	var list []string
	list = append(list, DefaultIgnores...)
	list = append(list, b.cfg.IgnorePatterns...)

	// Read .gitignore if present
	giPath := filepath.Join(root, ".gitignore")
	if data, err := os.ReadFile(giPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			list = append(list, line)
		}
	}
	return list
}

func (b *Bundler) isIgnored(rel string, isDir bool, patterns []string) bool {
	cleanRel := filepath.ToSlash(rel)
	base := filepath.Base(cleanRel)

	for _, p := range patterns {
		p = strings.TrimSpace(p)
		p = strings.TrimSuffix(p, "/")

		// Direct match on base name or full relative path
		if base == p || cleanRel == p {
			return true
		}

		// Prefix directory check
		if strings.HasPrefix(cleanRel, p+"/") {
			return true
		}

		// Glob match
		if matched, _ := filepath.Match(p, base); matched {
			return true
		}
		if matched, _ := filepath.Match(p, cleanRel); matched {
			return true
		}
	}
	return false
}

func (b *Bundler) buildTree(files []string) string {
	var sb strings.Builder
	for _, f := range files {
		sb.WriteString(f)
		sb.WriteString("\n")
	}
	return sb.String()
}

// isBinary inspects the first 512 bytes for NULL bytes or invalid UTF-8.
func isBinary(data []byte) bool {
	limit := 512
	if len(data) < limit {
		limit = len(data)
	}
	chunk := data[:limit]

	if bytes.IndexByte(chunk, 0) != -1 {
		return true
	}
	return !utf8.Valid(chunk)
}

// BundleDirectory is a high-level helper to bundle a directory into an output file.
func BundleDirectory(dir, outFile string) (*Stats, error) {
	b := New(Config{RootDir: dir})
	content, stats, err := b.Bundle()
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(outFile, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write bundle file: %w", err)
	}
	return stats, nil
}
