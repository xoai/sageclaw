package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachmentBar_Add(t *testing.T) {
	bar := NewAttachmentBar(NewTheme(ThemeDark))

	// Create a temp file.
	tmp, err := os.CreateTemp("", "test-attach-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("test content")
	tmp.Close()
	defer os.Remove(tmp.Name())

	if err := bar.Add(tmp.Name()); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if bar.Count() != 1 {
		t.Errorf("expected 1 attachment, got %d", bar.Count())
	}
}

func TestAttachmentBar_AddNonexistent(t *testing.T) {
	bar := NewAttachmentBar(NewTheme(ThemeDark))
	if err := bar.Add("/nonexistent/file.txt"); err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestAttachmentBar_AddDirectory(t *testing.T) {
	bar := NewAttachmentBar(NewTheme(ThemeDark))
	dir := os.TempDir()
	if err := bar.Add(dir); err == nil {
		t.Error("expected error for directory")
	}
}

func TestAttachmentBar_Clear(t *testing.T) {
	bar := NewAttachmentBar(NewTheme(ThemeDark))

	tmp, _ := os.CreateTemp("", "test-*.txt")
	tmp.Close()
	defer os.Remove(tmp.Name())

	bar.Add(tmp.Name())
	if bar.Count() != 1 {
		t.Fatal("expected 1 attachment")
	}

	bar.Clear()
	if bar.Count() != 0 {
		t.Error("expected 0 attachments after clear")
	}
}

func TestAttachmentBar_View(t *testing.T) {
	bar := NewAttachmentBar(NewTheme(ThemeDark))

	// Empty bar should produce nothing.
	if v := bar.View(80); v != "" {
		t.Error("empty bar should have empty view")
	}

	tmp, _ := os.CreateTemp("", "image-*.png")
	tmp.Close()
	defer os.Remove(tmp.Name())

	bar.Add(tmp.Name())
	view := bar.View(80)

	if view == "" {
		t.Error("non-empty bar should have a view")
	}
	name := filepath.Base(tmp.Name())
	if !strings.Contains(view, name) {
		t.Errorf("view should contain filename %q", name)
	}
}

func TestAttachmentBar_MarkUploaded(t *testing.T) {
	bar := NewAttachmentBar(NewTheme(ThemeDark))

	tmp, _ := os.CreateTemp("", "test-*.txt")
	tmp.Close()
	defer os.Remove(tmp.Name())

	bar.Add(tmp.Name())
	bar.MarkUploaded(0, "/uploads/test.txt")

	if !bar.attachments[0].Uploaded {
		t.Error("attachment should be marked uploaded")
	}
	if bar.attachments[0].URL != "/uploads/test.txt" {
		t.Error("URL should be set")
	}
}
