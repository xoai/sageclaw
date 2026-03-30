package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/security"
)

func mustSandbox(t *testing.T, dir string) *security.Sandbox {
	t.Helper()
	sb, err := security.NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	return sb
}

// mockMediaProvider implements provider.Provider and provider.ProviderCapabilities.
type mockMediaProvider struct {
	name   string
	caps   map[string]bool
	chatFn func(ctx context.Context, req *canonical.Request) (*canonical.Response, error)
}

func (m *mockMediaProvider) Name() string { return m.name }
func (m *mockMediaProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	return m.chatFn(ctx, req)
}
func (m *mockMediaProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMediaProvider) Supports(cap string) bool {
	return m.caps[cap]
}

func TestTryProviders_FirstSuccess(t *testing.T) {
	p1 := &mockMediaProvider{
		name: "provider1",
		caps: map[string]bool{provider.CapVision: true},
		chatFn: func(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
			return &canonical.Response{
				Messages: []canonical.Message{{
					Role:    "assistant",
					Content: []canonical.Content{{Type: "text", Text: "from provider1"}},
				}},
			}, nil
		},
	}
	p2 := &mockMediaProvider{
		name: "provider2",
		caps: map[string]bool{provider.CapVision: true},
		chatFn: func(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
			return &canonical.Response{
				Messages: []canonical.Message{{
					Role:    "assistant",
					Content: []canonical.Content{{Type: "text", Text: "from provider2"}},
				}},
			}, nil
		},
	}

	providers := []provider.Provider{p1, p2}
	result, err := tryProviders(context.Background(), providers, provider.CapVision,
		func(p provider.Provider) (*canonical.ToolResult, error) {
			resp, err := p.Chat(context.Background(), &canonical.Request{})
			if err != nil {
				return nil, err
			}
			return &canonical.ToolResult{Content: extractTextFromResponse(resp)}, nil
		})

	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "from provider1" {
		t.Errorf("expected first provider, got: %s", result.Content)
	}
}

func TestTryProviders_FallbackOnFailure(t *testing.T) {
	p1 := &mockMediaProvider{
		name: "failing",
		caps: map[string]bool{provider.CapVision: true},
		chatFn: func(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
			return nil, fmt.Errorf("provider1 down")
		},
	}
	p2 := &mockMediaProvider{
		name: "working",
		caps: map[string]bool{provider.CapVision: true},
		chatFn: func(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
			return &canonical.Response{
				Messages: []canonical.Message{{
					Role:    "assistant",
					Content: []canonical.Content{{Type: "text", Text: "from fallback"}},
				}},
			}, nil
		},
	}

	providers := []provider.Provider{p1, p2}
	result, err := tryProviders(context.Background(), providers, provider.CapVision,
		func(p provider.Provider) (*canonical.ToolResult, error) {
			resp, err := p.Chat(context.Background(), &canonical.Request{})
			if err != nil {
				return nil, err
			}
			return &canonical.ToolResult{Content: extractTextFromResponse(resp)}, nil
		})

	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "from fallback" {
		t.Errorf("expected fallback provider, got: %s", result.Content)
	}
}

func TestTryProviders_AllFail(t *testing.T) {
	p1 := &mockMediaProvider{
		name: "fail1",
		caps: map[string]bool{provider.CapVision: true},
		chatFn: func(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
			return nil, fmt.Errorf("fail1")
		},
	}
	p2 := &mockMediaProvider{
		name: "fail2",
		caps: map[string]bool{provider.CapVision: true},
		chatFn: func(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
			return nil, fmt.Errorf("fail2")
		},
	}

	providers := []provider.Provider{p1, p2}
	result, _ := tryProviders(context.Background(), providers, provider.CapVision,
		func(p provider.Provider) (*canonical.ToolResult, error) {
			resp, err := p.Chat(context.Background(), &canonical.Request{})
			if err != nil {
				return nil, err
			}
			return &canonical.ToolResult{Content: extractTextFromResponse(resp)}, nil
		})

	if result == nil || !result.IsError {
		t.Error("expected error result when all providers fail")
	}
}

func TestTryProviders_NoCapableProvider(t *testing.T) {
	p := &mockMediaProvider{
		name: "nocap",
		caps: map[string]bool{provider.CapDocument: true}, // has document, not vision
	}

	providers := []provider.Provider{p}
	result, _ := tryProviders(context.Background(), providers, provider.CapVision,
		func(p provider.Provider) (*canonical.ToolResult, error) {
			return &canonical.ToolResult{Content: "should not reach"}, nil
		})

	if result == nil || !result.IsError {
		t.Error("expected error when no capable provider exists")
	}
}

func TestTryProviders_SkipsIncapable(t *testing.T) {
	p1 := &mockMediaProvider{
		name: "nocap",
		caps: map[string]bool{provider.CapDocument: true},
	}
	p2 := &mockMediaProvider{
		name: "capable",
		caps: map[string]bool{provider.CapVision: true},
		chatFn: func(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
			return &canonical.Response{
				Messages: []canonical.Message{{
					Role:    "assistant",
					Content: []canonical.Content{{Type: "text", Text: "from capable"}},
				}},
			}, nil
		},
	}

	providers := []provider.Provider{p1, p2}
	result, err := tryProviders(context.Background(), providers, provider.CapVision,
		func(p provider.Provider) (*canonical.ToolResult, error) {
			resp, err := p.Chat(context.Background(), &canonical.Request{})
			if err != nil {
				return nil, err
			}
			return &canonical.ToolResult{Content: extractTextFromResponse(resp)}, nil
		})

	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "from capable" {
		t.Errorf("expected capable provider, got: %s", result.Content)
	}
}

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"https://example.com/image.png", true},
		{"http://example.com/image.png", true},
		{"./images/photo.png", false},
		{"/absolute/path/image.jpg", false},
		{"relative/path.webp", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isHTTPURL(tt.path)
		if got != tt.want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestReadImage_CustomQuestion(t *testing.T) {
	var capturedPrompt string
	p := &mockMediaProvider{
		name: "vision",
		caps: map[string]bool{provider.CapVision: true},
		chatFn: func(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
			// Capture the text prompt.
			for _, msg := range req.Messages {
				for _, c := range msg.Content {
					if c.Type == "text" {
						capturedPrompt = c.Text
					}
				}
			}
			return &canonical.Response{
				Messages: []canonical.Message{{
					Role:    "assistant",
					Content: []canonical.Content{{Type: "text", Text: "analysis result"}},
				}},
			}, nil
		},
	}

	// Create a test image file.
	dir := t.TempDir()
	imgPath := dir + "/test.png"
	// Write a minimal PNG (1x1 pixel).
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := writeFile(imgPath, pngData); err != nil {
		t.Fatal(err)
	}

	// Register with sandbox rooted at temp dir.
	reg := NewRegistry()
	sandbox := mustSandbox(t, dir)
	RegisterMedia(reg, sandbox, []provider.Provider{p})

	// Test with custom question.
	input, _ := json.Marshal(map[string]string{
		"path":     "test.png",
		"question": "What color is this?",
	})
	_, fn, _ := reg.Get("read_image")
	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if capturedPrompt != "What color is this?" {
		t.Errorf("expected custom question, got: %s", capturedPrompt)
	}

	// Test with default question.
	input, _ = json.Marshal(map[string]string{"path": "test.png"})
	result, err = fn(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if capturedPrompt != "Describe this image in detail. What do you see?" {
		t.Errorf("expected default question, got: %s", capturedPrompt)
	}
}
