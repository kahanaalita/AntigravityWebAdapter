package multimodal

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var dataURIRegex = regexp.MustCompile(`^data:image/([a-zA-Z0-9+]+);base64,(.+)$`)

// SaveDataURI decodes a data:image/...;base64 URL and saves it to a file in destDir.
func SaveDataURI(dataURI, destDir string) (string, error) {
	matches := dataURIRegex.FindStringSubmatch(strings.TrimSpace(dataURI))
	if len(matches) != 3 {
		return "", fmt.Errorf("invalid data URI format")
	}

	ext := matches[1]
	if ext == "jpeg" {
		ext = "jpg"
	}
	b64Payload := matches[2]

	data, err := base64.StdEncoding.DecodeString(b64Payload)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 image: %w", err)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	filename := fmt.Sprintf("img_%d_%d.%s", time.Now().UnixNano(), len(data), ext)
	filePath := filepath.Join(destDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write image file: %w", err)
	}

	return filePath, nil
}

// ExtractImagesFromPartsWithTemp scans raw content structures for image URLs and data URIs,
// returning both all valid image file paths and a list of temporary files created for data URIs.
func ExtractImagesFromPartsWithTemp(parts []interface{}, tmpDir string) (allFiles []string, tempFiles []string) {
	for _, part := range parts {
		m, ok := part.(map[string]interface{})
		if !ok {
			continue
		}

		pType, _ := m["type"].(string)
		if pType != "image_url" {
			continue
		}

		imgUrlObj, ok := m["image_url"].(map[string]interface{})
		if !ok {
			continue
		}

		rawUrl, _ := imgUrlObj["url"].(string)
		rawUrl = strings.TrimSpace(rawUrl)
		if rawUrl == "" {
			continue
		}

		if strings.HasPrefix(rawUrl, "data:image/") {
			fp, err := SaveDataURI(rawUrl, tmpDir)
			if err == nil && fp != "" {
				allFiles = append(allFiles, fp)
				tempFiles = append(tempFiles, fp)
			}
		} else if strings.HasPrefix(rawUrl, "file://") {
			fp := strings.TrimPrefix(rawUrl, "file://")
			if _, err := os.Stat(fp); err == nil {
				allFiles = append(allFiles, fp)
			}
		} else if filepath.IsAbs(rawUrl) {
			if _, err := os.Stat(rawUrl); err == nil {
				allFiles = append(allFiles, rawUrl)
			}
		} else {
			if _, err := os.Stat(rawUrl); err == nil {
				if abs, err := filepath.Abs(rawUrl); err == nil {
					allFiles = append(allFiles, abs)
				} else {
					allFiles = append(allFiles, rawUrl)
				}
			}
		}
	}

	return allFiles, tempFiles
}

// ExtractImagesFromParts scans raw content structures for image URLs and data URIs.
func ExtractImagesFromParts(parts []interface{}, tmpDir string) []string {
	allFiles, _ := ExtractImagesFromPartsWithTemp(parts, tmpDir)
	return allFiles
}

// ResolveImageInputs processes a slice of raw image paths, file URLs, or base64 data URIs.
// Decoded data URIs are saved as temporary files in tmpDir and tracked in tempFiles for later cleanup.
func ResolveImageInputs(rawInputs []string, tmpDir string) (allFiles []string, tempFiles []string, err error) {
	for _, raw := range rawInputs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		if strings.HasPrefix(raw, "data:image/") {
			fp, err := SaveDataURI(raw, tmpDir)
			if err != nil {
				return allFiles, tempFiles, fmt.Errorf("failed to save data URI: %w", err)
			}
			allFiles = append(allFiles, fp)
			tempFiles = append(tempFiles, fp)
		} else if strings.HasPrefix(raw, "file://") {
			fp := strings.TrimPrefix(raw, "file://")
			if _, err := os.Stat(fp); err != nil {
				return allFiles, tempFiles, fmt.Errorf("image file not found: %s: %w", fp, err)
			}
			allFiles = append(allFiles, fp)
		} else {
			absPath := raw
			if !filepath.IsAbs(raw) {
				if abs, err := filepath.Abs(raw); err == nil {
					absPath = abs
				}
			}
			if _, err := os.Stat(absPath); err != nil {
				return allFiles, tempFiles, fmt.Errorf("image file not found: %s: %w", raw, err)
			}
			allFiles = append(allFiles, absPath)
		}
	}
	return allFiles, tempFiles, nil
}
