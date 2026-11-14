package engine_test

import (
	"testing"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/engine"
)

func TestComputeSafeDelta_EmptyAndIdentical(t *testing.T) {
	// Empty strings
	if delta := engine.ComputeSafeDelta("", ""); delta != "" {
		t.Fatalf("expected empty delta for empty strings, got %q", delta)
	}
	if delta := engine.ComputeSafeDelta("hello", ""); delta != "" {
		t.Fatalf("expected empty delta when curr is empty, got %q", delta)
	}

	// Identical strings
	if delta := engine.ComputeSafeDelta("hello", "hello"); delta != "" {
		t.Fatalf("expected empty delta for identical strings, got %q", delta)
	}
	if delta := engine.ComputeSafeDelta("Привет мир", "Привет мир"); delta != "" {
		t.Fatalf("expected empty delta for identical Cyrillic strings, got %q", delta)
	}
}

func TestComputeSafeDelta_IncrementalAdditions(t *testing.T) {
	// From empty to first word
	delta := engine.ComputeSafeDelta("", "Hello")
	if delta != "Hello" {
		t.Fatalf("expected 'Hello', got %q", delta)
	}

	// Append word
	delta = engine.ComputeSafeDelta("Hello", "Hello World")
	if delta != " World" {
		t.Fatalf("expected ' World', got %q", delta)
	}

	// Append punctuation
	delta = engine.ComputeSafeDelta("Hello World", "Hello World!")
	if delta != "!" {
		t.Fatalf("expected '!', got %q", delta)
	}
}

func TestComputeSafeDelta_CyrillicUTF8(t *testing.T) {
	// Cyrillic single character additions
	prev := "Привет"
	curr := "Привет,"
	delta := engine.ComputeSafeDelta(prev, curr)
	if delta != "," {
		t.Fatalf("expected ',', got %q", delta)
	}

	prev = curr
	curr = "Привет, к"
	delta = engine.ComputeSafeDelta(prev, curr)
	if delta != " к" {
		t.Fatalf("expected ' к', got %q", delta)
	}

	prev = curr
	curr = "Привет, как дела?"
	delta = engine.ComputeSafeDelta(prev, curr)
	if delta != "ак дела?" {
		t.Fatalf("expected 'ак дела?', got %q", delta)
	}

	// Check boundary safety: multi-byte characters
	// In UTF-8, Russian characters are 2 bytes each
	var accumulated string
	target := "Тестирование потоковой передачи UTF-8 символов и рассуждений моделей."
	for _, r := range target {
		next := accumulated + string(r)
		d := engine.ComputeSafeDelta(accumulated, next)
		if d != string(r) {
			t.Fatalf("for rune %c, expected delta %q, got %q", r, string(r), d)
		}
		accumulated = next
	}
	if accumulated != target {
		t.Fatalf("accumulated string mismatch: got %q, want %q", accumulated, target)
	}
}

func TestComputeSafeDelta_MarkdownReRender(t *testing.T) {
	// Markdown re-renders where prefix changes slightly (e.g. list marker or whitespace)
	// Example: DOM collapses whitespace or converts "* item" to bullet point
	prev := "* Первое правило бойцовского клуба"
	curr := "- Первое правило бойцовского клуба: никому не рассказывать"
	delta := engine.ComputeSafeDelta(prev, curr)
	if delta != curr {
		t.Fatalf("expected full curr %q, got %q", curr, delta)
	}

	// Case where common prefix matches until the end of prev
	prev = "## Заголовок статьи\nТекст"
	curr = "## Заголовок статьи\nТекст продолжается"
	delta = engine.ComputeSafeDelta(prev, curr)
	if delta != " продолжается" {
		t.Fatalf("expected ' продолжается', got %q", delta)
	}

	// Case where curr is shorter than prev (DOM re-render temporary contraction)
	prev = "Длинный текст ответа модели"
	curr = "Длинный текст"
	delta = engine.ComputeSafeDelta(prev, curr)
	if delta != "" {
		t.Fatalf("expected empty delta when curr is shorter than prev, got %q", delta)
	}
}
