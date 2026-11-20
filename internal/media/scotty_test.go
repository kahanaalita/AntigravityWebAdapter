package media

import (
	"os"
	"strings"
	"testing"
)

func TestGetScottyPushID(t *testing.T) {
	origVal, exists := os.LookupEnv("SCOTTY_PUSH_ID")
	defer func() {
		if exists {
			os.Setenv("SCOTTY_PUSH_ID", origVal)
		} else {
			os.Unsetenv("SCOTTY_PUSH_ID")
		}
	}()

	// 1. Unset env should return default
	os.Unsetenv("SCOTTY_PUSH_ID")
	if id := GetScottyPushID(); id != defaultScottyPushID {
		t.Errorf("Expected default %q, got %q", defaultScottyPushID, id)
	}

	// 2. Empty or whitespace env should return default
	os.Setenv("SCOTTY_PUSH_ID", "   ")
	if id := GetScottyPushID(); id != defaultScottyPushID {
		t.Errorf("Expected default for whitespace, got %q", id)
	}

	// 3. Custom env should be returned
	customID := "feeds/custom-push-feed-12345"
	os.Setenv("SCOTTY_PUSH_ID", customID)
	if id := GetScottyPushID(); id != customID {
		t.Errorf("Expected custom %q, got %q", customID, id)
	}
}

func TestResolveClipboardLinux_MissingUtilities(t *testing.T) {
	resolver := NewImageResolver("")
	_, err := resolver.resolveClipboardLinux()
	if err == nil {
		return
	}
	if !strings.Contains(err.Error(), "Linux clipboard") && !strings.Contains(err.Error(), "wl-paste") && !strings.Contains(err.Error(), "xclip") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestResolveClipboardFromCommand(t *testing.T) {
	// 1. Non-existent command
	_, err := resolveClipboardFromCommand("non_existent_clip_tool_xyz_9999")
	if err == nil {
		t.Errorf("Expected error for non-existent command, got nil")
	}

	// 2. Command with empty stdout
	_, err = resolveClipboardFromCommand("true")
	if err == nil {
		t.Errorf("Expected error for command with empty output, got nil")
	}

	// 3. Command with output
	img, err := resolveClipboardFromCommand("echo", "sample_image_data")
	if err != nil {
		t.Fatalf("Expected success for echo command, got: %v", err)
	}
	if string(img.Data) != "sample_image_data\n" {
		t.Errorf("Unexpected data from command: %q", string(img.Data))
	}
	if img.FileName != "clipboard.png" {
		t.Errorf("Expected clipboard.png, got %s", img.FileName)
	}
}
