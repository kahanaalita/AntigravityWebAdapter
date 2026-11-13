package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWizGlobalData(t *testing.T) {
	html := `<!doctype html><html><head><script>
window.WIZ_global_data = {
  "SNlM0e":"sec_token_abcdef_12345",
  "cfb2h":"boq_assistant-bard-web-server_20261113.00_p0",
  "FdrFJe":"session_sid_998877"
};
</script></head><body></body></html>`

	wiz, err := ParseWizGlobalData(html)
	if err != nil {
		t.Fatalf("ParseWizGlobalData failed: %v", err)
	}

	if wiz.At != "sec_token_abcdef_12345" {
		t.Errorf("Expected At 'sec_token_abcdef_12345', got %q", wiz.At)
	}
	if wiz.Bl != "boq_assistant-bard-web-server_20261113.00_p0" {
		t.Errorf("Expected Bl 'boq_assistant-bard-web-server_20261113.00_p0', got %q", wiz.Bl)
	}
	if wiz.Sid != "session_sid_998877" {
		t.Errorf("Expected Sid 'session_sid_998877', got %q", wiz.Sid)
	}

	// Missing SNlM0e should return error
	missingHTML := `<html><body>No tokens here</body></html>`
	_, err = ParseWizGlobalData(missingHTML)
	if err == nil {
		t.Errorf("Expected error when SNlM0e is missing")
	}
}

func TestCookieStore_Formats(t *testing.T) {
	tmpDir := t.TempDir()

	// Format 1: Map
	mapPath := filepath.Join(tmpDir, "map.json")
	if err := os.WriteFile(mapPath, []byte(`{"__Secure-1PSID":"val1","__Secure-1PSIDTS":"val2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store1 := NewCookieStore(mapPath)
	if err := store1.Load(); err != nil {
		t.Fatalf("Load map failed: %v", err)
	}
	v, ok := store1.Get("__Secure-1PSID")
	if !ok || v != "val1" {
		t.Errorf("Expected val1, got %v", v)
	}

	// Format 2: Array of cookie objects
	arrPath := filepath.Join(tmpDir, "arr.json")
	if err := os.WriteFile(arrPath, []byte(`[{"name":"__Secure-1PSID","value":"valA"},{"name":"SID","value":"valB"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	store2 := NewCookieStore(arrPath)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load array failed: %v", err)
	}
	v, ok = store2.Get("__Secure-1PSID")
	if !ok || v != "valA" {
		t.Errorf("Expected valA, got %v", v)
	}
	v, ok = store2.Get("SID")
	if !ok || v != "valB" {
		t.Errorf("Expected valB, got %v", v)
	}

	// Format 3: Raw string
	rawPath := filepath.Join(tmpDir, "raw.txt")
	if err := os.WriteFile(rawPath, []byte(`__Secure-1PSID=valX; __Secure-1PSIDTS=valY`), 0600); err != nil {
		t.Fatal(err)
	}
	store3 := NewCookieStore(rawPath)
	if err := store3.Load(); err != nil {
		t.Fatalf("Load raw failed: %v", err)
	}
	v, ok = store3.Get("__Secure-1PSID")
	if !ok || v != "valX" {
		t.Errorf("Expected valX, got %v", v)
	}

	// Test Save & CookieHeader
	savePath := filepath.Join(tmpDir, "save.json")
	store4 := NewCookieStore(savePath)
	store4.Set("A", "1")
	store4.Set("B", "2")
	if err := store4.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	hdr := store4.CookieHeader()
	if !strings.Contains(hdr, "A=1") || !strings.Contains(hdr, "B=2") {
		t.Errorf("CookieHeader missing entries: %q", hdr)
	}
}
