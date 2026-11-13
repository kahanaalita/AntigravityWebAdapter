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

// ExtractImagesFromParts scans raw content structures for image URLs and data URIs.
func ExtractImagesFromParts(parts []interface{}, tmpDir string) []string {
	var filePaths []string

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
				filePaths = append(filePaths, fp)
			}
		} else if strings.HasPrefix(rawUrl, "file://") {
			fp := strings.TrimPrefix(rawUrl, "file://")
			if _, err := os.Stat(fp); err == nil {
				filePaths = append(filePaths, fp)
			}
		} else if filepath.IsAbs(rawUrl) {
			if _, err := os.Stat(rawUrl); err == nil {
				filePaths = append(filePaths, rawUrl)
			}
		}
	}

	return filePaths
}
