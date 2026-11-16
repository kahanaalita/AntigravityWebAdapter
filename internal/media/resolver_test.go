package media

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fhttp "github.com/bogdanfinn/fhttp"
)

func TestResolveFromPromptAndList(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "sample.png")
	// Minimal valid PNG header
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if err := os.WriteFile(imgPath, pngData, 0600); err != nil {
		t.Fatal(err)
	}

	resolver := NewImageResolver(tmpDir)

	prompt := "Что на картинке @" + filepath.Base(imgPath) + " объясни подробно"
	cleaned, imgs, err := resolver.ResolveFromPromptAndList(prompt, nil)
	if err != nil {
		t.Fatalf("ResolveFromPromptAndList failed: %v", err)
	}

	if len(imgs) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(imgs))
	}
	if imgs[0].FileName != "sample.png" {
		t.Errorf("Expected sample.png, got %s", imgs[0].FileName)
	}
	if imgs[0].MimeType != "image/png" {
		t.Errorf("Expected image/png, got %s", imgs[0].MimeType)
	}
	if strings.Contains(cleaned, "@sample.png") {
		t.Errorf("Cleaned prompt should not contain @sample.png: %q", cleaned)
	}
	if !strings.Contains(cleaned, "Что на картинке объясни подробно") {
		t.Errorf("Cleaned prompt mismatch: %q", cleaned)
	}
}

func TestResolveDataURI(t *testing.T) {
	resolver := NewImageResolver("")
	raw := []byte("hello image content")
	b64 := base64.StdEncoding.EncodeToString(raw)
	uri := "data:image/jpeg;base64," + b64

	cleaned, imgs, err := resolver.ResolveFromPromptAndList("check this", []string{uri})
	if err != nil {
		t.Fatalf("ResolveDataURI failed: %v", err)
	}

	if len(imgs) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(imgs))
	}
	if imgs[0].MimeType != "image/jpeg" {
		t.Errorf("Expected image/jpeg, got %s", imgs[0].MimeType)
	}
	if string(imgs[0].Data) != string(raw) {
		t.Errorf("Data mismatch")
	}
	if cleaned != "check this" {
		t.Errorf("Cleaned mismatch: %q", cleaned)
	}
}

func TestExtractUploadRef(t *testing.T) {
	// From JSON resource url
	jsonBody := `{"sessionStatus":{"state":"FINALIZED"},"resource":{"url":"/contrib_service/ttl_1d/17390001_abc"}}`
	hdr := make(fhttp.Header)
	ref := extractUploadRef(jsonBody, hdr)
	if ref != "/contrib_service/ttl_1d/17390001_abc" {
		t.Errorf("Expected /contrib_service/ttl_1d/17390001_abc, got %q", ref)
	}

	// From plain body
	plainBody := `/contrib_service/ttl_1d/plain_ref_999`
	ref = extractUploadRef(plainBody, hdr)
	if ref != "/contrib_service/ttl_1d/plain_ref_999" {
		t.Errorf("Expected /contrib_service/ttl_1d/plain_ref_999, got %q", ref)
	}

	// From Header
	hdr.Set("X-Goog-Upload-Status-Resource-Id", "/contrib_service/ttl_1d/header_ref_888")
	ref = extractUploadRef("", hdr)
	if ref != "/contrib_service/ttl_1d/header_ref_888" {
		t.Errorf("Expected /contrib_service/ttl_1d/header_ref_888, got %q", ref)
	}
}

func TestResolveFromPrompt_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewImageResolver(tmpDir)

	prompt := "Please check @nonexistent_diagram.png and contact user@domain.png for info"
	cleaned, imgs, err := resolver.ResolveFromPromptAndList(prompt, nil)
	if err != nil {
		t.Fatalf("Expected no error for nonexistent file in prompt, got: %v", err)
	}
	if len(imgs) != 0 {
		t.Fatalf("Expected 0 images, got %d", len(imgs))
	}
	if !strings.Contains(cleaned, "@nonexistent_diagram.png") {
		t.Errorf("Prompt should preserve nonexistent token, got: %q", cleaned)
	}
	if !strings.Contains(cleaned, "user@domain.png") {
		t.Errorf("Prompt should preserve user@domain.png, got: %q", cleaned)
	}
}

func TestResolveFromPrompt_MixedExistentAndNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "real.png")
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if err := os.WriteFile(imgPath, pngData, 0600); err != nil {
		t.Fatal(err)
	}

	resolver := NewImageResolver(tmpDir)
	prompt := "Compare @real.png with @missing.png please"
	cleaned, imgs, err := resolver.ResolveFromPromptAndList(prompt, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(imgs))
	}
	if imgs[0].FileName != "real.png" {
		t.Errorf("Expected real.png, got %s", imgs[0].FileName)
	}
	if strings.Contains(cleaned, "@real.png") {
		t.Errorf("Cleaned prompt should not contain @real.png: %q", cleaned)
	}
	if !strings.Contains(cleaned, "@missing.png") {
		t.Errorf("Cleaned prompt must retain @missing.png: %q", cleaned)
	}
}

func TestResolveExplicit_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewImageResolver(tmpDir)

	_, _, err := resolver.ResolveFromPromptAndList("some prompt", []string{"definitely_missing.png"})
	if err == nil {
		t.Fatalf("Expected error for explicitly requested nonexistent file, got nil")
	}
}
