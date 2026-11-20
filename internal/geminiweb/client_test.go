package geminiweb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConversationStore_LRUAndCapacity(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "conversations.json")

	// Capacity: 3
	store := NewConversationStore(3, time.Hour, filePath)

	store.Put("conv_1", ConversationContext{ConversationID: "conv_1", ResponseID: "resp_1"})
	store.Put("conv_2", ConversationContext{ConversationID: "conv_2", ResponseID: "resp_2"})
	store.Put("conv_3", ConversationContext{ConversationID: "conv_3", ResponseID: "resp_3"})

	if store.Len() != 3 {
		t.Fatalf("Expected 3 items in store, got %d", store.Len())
	}

	// Access conv_1 so it becomes most recently used
	ctx1, ok := store.Get("conv_1")
	if !ok || ctx1.ResponseID != "resp_1" {
		t.Fatalf("Failed to retrieve conv_1: %+v", ctx1)
	}

	// Add conv_4: should evict conv_2 (since conv_1 was accessed, conv_2 is now the oldest)
	store.Put("conv_4", ConversationContext{ConversationID: "conv_4", ResponseID: "resp_4"})

	if store.Len() != 3 {
		t.Fatalf("Expected 3 items after eviction, got %d", store.Len())
	}

	// conv_2 must be evicted
	if _, exists := store.Get("conv_2"); exists {
		t.Errorf("Expected conv_2 to be evicted by LRU")
	}

	// conv_1, conv_3, conv_4 must exist
	for _, id := range []string{"conv_1", "conv_3", "conv_4"} {
		if _, exists := store.Get(id); !exists {
			t.Errorf("Expected %s to exist in store", id)
		}
	}
}

func TestConversationStore_TTLExpiration(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "conversations.json")

	// Very short TTL: 50ms
	store := NewConversationStore(10, 50*time.Millisecond, filePath)

	store.Put("conv_fast", ConversationContext{ConversationID: "conv_fast", ResponseID: "resp_fast"})

	// Immediate check should succeed
	if ctx, ok := store.Get("conv_fast"); !ok || ctx.ResponseID != "resp_fast" {
		t.Fatalf("Failed immediate retrieval: %+v", ctx)
	}

	// Wait for TTL to expire
	time.Sleep(80 * time.Millisecond)

	// After TTL, Get should return false and clean up entry
	if _, ok := store.Get("conv_fast"); ok {
		t.Errorf("Expected conv_fast to be expired by TTL")
	}

	if store.Len() != 0 {
		t.Errorf("Expected store length 0 after expired entry lookup, got %d", store.Len())
	}
}

func TestConversationStore_FilePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "conversations.json")

	// 1. Create store and add conversations
	store1 := NewConversationStore(10, 24*time.Hour, filePath)
	store1.Put("c_alpha", ConversationContext{ConversationID: "c_alpha", ResponseID: "r_alpha", ChoiceID: "rc_alpha"})
	store1.Put("c_beta", ConversationContext{ConversationID: "c_beta", ResponseID: "r_beta", ChoiceID: "rc_beta"})

	// Verify file was written
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("Expected conversations file to be created at %s", filePath)
	}

	// 2. Simulate server restart: create a new store reading the same file
	store2 := NewConversationStore(10, 24*time.Hour, filePath)

	if store2.Len() != 2 {
		t.Fatalf("Expected 2 items loaded from disk, got %d", store2.Len())
	}

	alpha, ok := store2.Get("c_alpha")
	if !ok || alpha.ResponseID != "r_alpha" || alpha.ChoiceID != "rc_alpha" {
		t.Errorf("c_alpha corrupted or missing after load: %+v", alpha)
	}

	beta, ok := store2.Get("c_beta")
	if !ok || beta.ResponseID != "r_beta" || beta.ChoiceID != "rc_beta" {
		t.Errorf("c_beta corrupted or missing after load: %+v", beta)
	}
}

func TestConversationStore_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "conversations.json")
	store := NewConversationStore(100, time.Hour, filePath)

	var wg sync.WaitGroup
	workers := 20
	iterations := 50

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := fmt.Sprintf("conv_%d_%d", workerID, i%5)
				store.Put(key, ConversationContext{
					ConversationID: key,
					ResponseID:     fmt.Sprintf("resp_%d_%d", workerID, i),
				})
				_, _ = store.Get(key)
			}
		}(w)
	}

	wg.Wait()
	if store.Len() == 0 {
		t.Errorf("Expected store to have items after concurrent operations")
	}
}

func TestAuthErrorDetection(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "explicit ErrAuthenticationRequired",
			err:      ErrAuthenticationRequired,
			expected: true,
		},
		{
			name:     "wrapped ErrAuthenticationRequired",
			err:      fmt.Errorf("call failed: %w", ErrAuthenticationRequired),
			expected: true,
		},
		{
			name:     "accounts.google.com redirect error",
			err:      errors.New("gemini session expired: redirected to accounts.google.com"),
			expected: true,
		},
		{
			name:     "ServiceLogin error",
			err:      errors.New("redirect status 302 to https://accounts.google.com/ServiceLogin"),
			expected: true,
		},
		{
			name:     "general network error",
			err:      errors.New("connection reset by peer"),
			expected: false,
		},
		{
			name:     "google api 429 quota error",
			err:      errors.New("google api error 429 [RESOURCE_EXHAUSTED]: Resource exhausted"),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := isAuthError(tc.err)
			if got != tc.expected {
				t.Errorf("isAuthError(%v) = %v; want %v", tc.err, got, tc.expected)
			}
		})
	}
}

func TestClient_ConversationPersistenceDelegation(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "conversations.json")

	store := NewConversationStore(10, time.Hour, filePath)
	client := NewClientWithStore(nil, store)

	client.SaveConversationContext("c_test_01", ConversationContext{
		ConversationID: "c_test_01",
		ResponseID:     "r_test_01",
		ChoiceID:       "rc_test_01",
	})

	ctx := client.GetConversationContext("c_test_01")
	if ctx.ConversationID != "c_test_01" || ctx.ResponseID != "r_test_01" || ctx.ChoiceID != "rc_test_01" {
		t.Errorf("Client delegation failed to retrieve saved context: %+v", ctx)
	}

	// Verify it wrote to disk
	reloadedStore := NewConversationStore(10, time.Hour, filePath)
	reloadedCtx, ok := reloadedStore.Get("c_test_01")
	if !ok || reloadedCtx.ResponseID != "r_test_01" {
		t.Errorf("Reloaded store failed to find context saved by Client: %+v", reloadedCtx)
	}
}
