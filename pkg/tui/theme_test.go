package tui

import (
	"os"
	"testing"
)

func TestDetectTheme_Default(t *testing.T) {
	os.Unsetenv("SAGECLAW_THEME")
	os.Unsetenv("COLORFGBG")

	mode := DetectTheme()
	if mode != ThemeDark {
		t.Errorf("expected ThemeDark, got %d", mode)
	}
}

func TestDetectTheme_EnvOverride(t *testing.T) {
	t.Setenv("SAGECLAW_THEME", "light")
	os.Unsetenv("COLORFGBG")

	mode := DetectTheme()
	if mode != ThemeLight {
		t.Errorf("expected ThemeLight, got %d", mode)
	}
}

func TestDetectTheme_EnvOverrideDark(t *testing.T) {
	t.Setenv("SAGECLAW_THEME", "dark")

	mode := DetectTheme()
	if mode != ThemeDark {
		t.Errorf("expected ThemeDark, got %d", mode)
	}
}

func TestDetectTheme_COLORFGBG_Light(t *testing.T) {
	os.Unsetenv("SAGECLAW_THEME")
	t.Setenv("COLORFGBG", "0;15")

	mode := DetectTheme()
	if mode != ThemeLight {
		t.Errorf("expected ThemeLight for COLORFGBG=0;15, got %d", mode)
	}
}

func TestDetectTheme_COLORFGBG_Dark(t *testing.T) {
	os.Unsetenv("SAGECLAW_THEME")
	t.Setenv("COLORFGBG", "15;0")

	mode := DetectTheme()
	if mode != ThemeDark {
		t.Errorf("expected ThemeDark for COLORFGBG=15;0, got %d", mode)
	}
}

func TestNewTheme_Dark(t *testing.T) {
	theme := NewTheme(ThemeDark)
	if theme.Text == "" {
		t.Error("dark theme text color should be set")
	}
	if theme.Header.GetBold() != true {
		t.Error("header should be bold")
	}
}

func TestNewTheme_Light(t *testing.T) {
	theme := NewTheme(ThemeLight)
	if theme.Text == "" {
		t.Error("light theme text color should be set")
	}
}
