package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/security"
)

func setupEditTest(t *testing.T) (*security.Sandbox, string) {
	t.Helper()
	dir := t.TempDir()
	sb, err := security.NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	return sb, dir
}

func TestEdit_SingleReplace(t *testing.T) {
	sb, dir := setupEditTest(t)
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	reg := NewRegistry()
	RegisterEdit(reg, sb)

	input := json.RawMessage(`{"path":"test.txt","old_string":"hello","new_string":"goodbye"}`)
	result, err := reg.Execute(context.Background(), "edit", input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "replaced 1 occurrence") {
		t.Errorf("unexpected result: %s", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "goodbye world" {
		t.Errorf("file content wrong: %s", string(data))
	}
}

func TestEdit_NotFound(t *testing.T) {
	sb, dir := setupEditTest(t)
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	reg := NewRegistry()
	RegisterEdit(reg, sb)

	input := json.RawMessage(`{"path":"test.txt","old_string":"xyz","new_string":"abc"}`)
	result, _ := reg.Execute(context.Background(), "edit", input)
	if !result.IsError {
		t.Fatal("expected error for not found")
	}
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected 'not found' message, got: %s", result.Content)
	}
}

func TestEdit_MultipleMatchesNoReplaceAll(t *testing.T) {
	sb, dir := setupEditTest(t)
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("foo bar foo baz foo"), 0644)

	reg := NewRegistry()
	RegisterEdit(reg, sb)

	input := json.RawMessage(`{"path":"test.txt","old_string":"foo","new_string":"qux"}`)
	result, _ := reg.Execute(context.Background(), "edit", input)
	if !result.IsError {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(result.Content, "3 times") {
		t.Errorf("expected match count, got: %s", result.Content)
	}
}

func TestEdit_ReplaceAll(t *testing.T) {
	sb, dir := setupEditTest(t)
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("foo bar foo baz foo"), 0644)

	reg := NewRegistry()
	RegisterEdit(reg, sb)

	input := json.RawMessage(`{"path":"test.txt","old_string":"foo","new_string":"qux","replace_all":true}`)
	result, err := reg.Execute(context.Background(), "edit", input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "replaced 3 occurrence") {
		t.Errorf("unexpected result: %s", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "qux bar qux baz qux" {
		t.Errorf("file content wrong: %s", string(data))
	}
}

func TestEdit_EmptyOldString(t *testing.T) {
	sb, _ := setupEditTest(t)
	reg := NewRegistry()
	RegisterEdit(reg, sb)

	input := json.RawMessage(`{"path":"test.txt","old_string":"","new_string":"abc"}`)
	result, _ := reg.Execute(context.Background(), "edit", input)
	if !result.IsError {
		t.Fatal("expected error for empty old_string")
	}
}

func TestBinaryFileDetection(t *testing.T) {
	tests := []struct {
		path   string
		binary bool
	}{
		{"image.png", true},
		{"doc.pdf", true},
		{"archive.zip", true},
		{"code.go", false},
		{"readme.md", false},
		{"data.json", false},
		{"font.woff2", true},
		{"movie.mp4", true},
		{"db.sqlite", true},
	}
	for _, tt := range tests {
		if got := isBinaryFileExt(tt.path); got != tt.binary {
			t.Errorf("isBinaryFileExt(%q) = %v, want %v", tt.path, got, tt.binary)
		}
	}
}
