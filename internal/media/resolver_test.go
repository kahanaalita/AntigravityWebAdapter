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

	// From Header (lowercase HTTP/2 wire format)
	hdrLower := make(fhttp.Header)
	hdrLower["x-goog-upload-status-resource-id"] = []string{"/contrib_service/ttl_1d/http2_lower_ref_777"}
	refLower := extractUploadRef("", hdrLower)
	if refLower != "/contrib_service/ttl_1d/http2_lower_ref_777" {
		t.Errorf("Expected /contrib_service/ttl_1d/http2_lower_ref_777, got %q", refLower)
	}

	// From Header (uppercase)
	hdrUpper := make(fhttp.Header)
	hdrUpper["X-GOOG-UPLOAD-STATUS-RESOURCE-ID"] = []string{"/contrib_service/ttl_1d/http2_upper_ref_666"}
	refUpper := extractUploadRef("", hdrUpper)
	if refUpper != "/contrib_service/ttl_1d/http2_upper_ref_666" {
		t.Errorf("Expected /contrib_service/ttl_1d/http2_upper_ref_666, got %q", refUpper)
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

func TestResolveClipboard_EmptyOrErrorDoesNotCrash(t *testing.T) {
	resolver := NewImageResolver("")
	prompt := "Please examine @clipboard for issues"
	cleaned, imgs, err := resolver.ResolveFromPromptAndList(prompt, nil)
	if err != nil {
		t.Fatalf("ResolveFromPromptAndList should not fail on empty clipboard: %v", err)
	}
	if strings.Contains(cleaned, "@clipboard") {
		t.Errorf("Cleaned prompt should not contain @clipboard: %q", cleaned)
	}
	if !strings.Contains(cleaned, "Please examine for issues") {
		t.Errorf("Cleaned prompt mismatch: %q", cleaned)
	}
	_ = imgs // May or may not contain an image depending on actual clipboard state in OS, but must not error
}

func TestResolveClipboard_IsolatedTokenMatching(t *testing.T) {
	resolver := NewImageResolver("")
	prompt := "Check user@clipboard.com and @clipboard-special together with @clipboard please"
	cleaned, _, err := resolver.ResolveFromPromptAndList(prompt, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !strings.Contains(cleaned, "user@clipboard.com") {
		t.Errorf("Prompt should preserve user@clipboard.com: %q", cleaned)
	}
	if !strings.Contains(cleaned, "@clipboard-special") {
		t.Errorf("Prompt should preserve @clipboard-special: %q", cleaned)
	}
	if strings.Contains(cleaned, " together with @clipboard please") {
		t.Errorf("Isolated @clipboard should have been removed: %q", cleaned)
	}
}

func TestResolve_DeduplicationBySha256(t *testing.T) {
	tmpDir := t.TempDir()
	imgData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01}

	file1 := filepath.Join(tmpDir, "file1.png")
	file2 := filepath.Join(tmpDir, "file2.png")
	if err := os.WriteFile(file1, imgData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, imgData, 0600); err != nil {
		t.Fatal(err)
	}

	resolver := NewImageResolver(tmpDir)

	// Explicit duplicate paths with same content
	_, imgs, err := resolver.ResolveFromPromptAndList("test prompt", []string{file1, file2})
	if err != nil {
		t.Fatalf("ResolveFromPromptAndList failed: %v", err)
	}
	if len(imgs) != 1 {
		t.Errorf("Expected 1 deduplicated image, got %d", len(imgs))
	}

	// Mixed prompt reference and explicit list reference to same file
	prompt := "Check @" + filepath.Base(file1) + " please"
	cleaned, imgsMixed, err := resolver.ResolveFromPromptAndList(prompt, []string{file2})
	if err != nil {
		t.Fatalf("ResolveFromPromptAndList mixed failed: %v", err)
	}
	if len(imgsMixed) != 1 {
		t.Errorf("Expected 1 deduplicated image for mixed source, got %d", len(imgsMixed))
	}
	if strings.Contains(cleaned, "@file1.png") {
		t.Errorf("Prompt should have removed @file1.png: %q", cleaned)
	}
}

func TestExtractUploadRef_RupioNestedJson(t *testing.T) {
	hdr := make(fhttp.Header)

	// Nested photopicker_service_attachment_token (modern Google Rupio)
	rupioBody := `{
		"sessionStatus": {
			"state": "FINALIZED",
			"additionalInfo": {
				"uploader_service.GoogleRupioAdditionalInfo": {
					"customerSpecificInfo": {
						"photopicker_service_attachment_token": "CAESY2F0X3Rva2VuXzEyMzQ1Njc4OQ"
					}
				}
			}
		}
	}`
	ref := extractUploadRef(rupioBody, hdr)
	if ref != "CAESY2F0X3Rva2VuXzEyMzQ1Njc4OQ" {
		t.Errorf("Expected photopicker token, got %q", ref)
	}

	// Direct resourceId
	resIDBody := `{"sessionStatus": {"state": "FINALIZED"}, "resourceId": "res_id_abc_123"}`
	ref = extractUploadRef(resIDBody, hdr)
	if ref != "res_id_abc_123" {
		t.Errorf("Expected resourceId, got %q", ref)
	}

	// Direct upload_ref
	uploadRefBody := `{"upload_ref": "upload_ref_xyz_789"}`
	ref = extractUploadRef(uploadRefBody, hdr)
	if ref != "upload_ref_xyz_789" {
		t.Errorf("Expected upload_ref, got %q", ref)
	}

	// resource.url fallback
	resURLBody := `{"resource": {"url": "https://gemini.google.com/upload/photo_001"}}`
	ref = extractUploadRef(resURLBody, hdr)
	if ref != "https://gemini.google.com/upload/photo_001" {
		t.Errorf("Expected resource.url, got %q", ref)
	}
}

func TestResolveFromPromptAndList_QuotedPathsWithSpaces(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "Screenshots")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	img1 := filepath.Join(subDir, "App UI.png")
	if err := os.WriteFile(img1, pngData, 0600); err != nil {
		t.Fatal(err)
	}
	img2 := filepath.Join(subDir, "Dark Mode.PNG")
	if err := os.WriteFile(img2, append(pngData, 0x01), 0600); err != nil {
		t.Fatal(err)
	}

	resolver := NewImageResolver(tmpDir)

	// 1. Double quotes with spaces in prompt
	prompt := `Проанализируй @"Screenshots/App UI.png" и скажи вердикт`
	cleaned, imgs, err := resolver.ResolveFromPromptAndList(prompt, nil)
	if err != nil {
		t.Fatalf("ResolveFromPromptAndList failed: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(imgs))
	}
	if imgs[0].FileName != "App UI.png" {
		t.Errorf("Expected 'App UI.png', got %s", imgs[0].FileName)
	}
	if cleaned != "Проанализируй и скажи вердикт" {
		t.Errorf("Cleaned prompt mismatch: %q", cleaned)
	}

	// 2. Single quotes with spaces and uppercase extension in prompt
	promptSingle := `Взгляни на @'Screenshots/Dark Mode.PNG' внимательно`
	cleanedSingle, imgsSingle, err := resolver.ResolveFromPromptAndList(promptSingle, nil)
	if err != nil {
		t.Fatalf("ResolveFromPromptAndList single failed: %v", err)
	}
	if len(imgsSingle) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(imgsSingle))
	}
	if imgsSingle[0].FileName != "Dark Mode.PNG" {
		t.Errorf("Expected 'Dark Mode.PNG', got %s", imgsSingle[0].FileName)
	}
	if cleanedSingle != "Взгляни на внимательно" {
		t.Errorf("Cleaned prompt mismatch: %q", cleanedSingle)
	}

	// 3. Explicit paths with quotes and @ prefix
	explicitList := []string{
		`@"Screenshots/App UI.png"`,
		`'Screenshots/Dark Mode.PNG'`,
	}
	cleanedExplicit, imgsExplicit, err := resolver.ResolveFromPromptAndList("test explicit", explicitList)
	if err != nil {
		t.Fatalf("ResolveFromPromptAndList explicit failed: %v", err)
	}
	if len(imgsExplicit) != 2 {
		t.Fatalf("Expected 2 images, got %d", len(imgsExplicit))
	}
	if cleanedExplicit != "test explicit" {
		t.Errorf("Cleaned prompt mismatch: %q", cleanedExplicit)
	}

	// 4. Non-existent quoted file should stay in prompt
	nonExistentPrompt := `Check @"Screenshots/Missing File.png" please`
	cleanedNE, imgsNE, err := resolver.ResolveFromPromptAndList(nonExistentPrompt, nil)
	if err != nil {
		t.Fatalf("Unexpected error for non-existent quoted file: %v", err)
	}
	if len(imgsNE) != 0 {
		t.Fatalf("Expected 0 images, got %d", len(imgsNE))
	}
	if !strings.Contains(cleanedNE, `@"Screenshots/Missing File.png"`) {
		t.Errorf("Cleaned prompt must retain non-existent token, got: %q", cleanedNE)
	}
}
