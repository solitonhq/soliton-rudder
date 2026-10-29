package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsWhenMissing(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Theme; got != ThemeDarkBlack {
		t.Fatalf("theme = %q, want %q", got, ThemeDarkBlack)
	}
}

func TestSetThemePersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "s.json")
	s, _ := Open(p)
	if err := s.SetTheme(ThemeLight); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get().Theme; got != ThemeLight {
		t.Fatalf("theme = %q, want light", got)
	}
}

func TestInvalidThemeRejected(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "s.json"))
	if err := s.SetTheme("neon"); err == nil {
		t.Fatal("expected error")
	}
	if s.Get().Theme != ThemeDarkBlack {
		t.Fatal("theme changed on invalid input")
	}
}

func TestCorruptFileFallsBackToDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(p, []byte("{not json"), 0o600)
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Get().Theme != ThemeDarkBlack {
		t.Fatal("expected defaults")
	}
}

func TestOlderFileKeepsNewDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(p, []byte(`{"theme":"light"}`), 0o600) // written by v0 before CheckUpdates existed
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.Theme != ThemeLight || !got.CheckUpdates {
		t.Fatalf("%+v", got)
	}
}

func TestUpdateFlagsPersistAndReset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	s, _ := Open(p)
	_ = s.SetCheckUpdates(false)
	_ = s.SetLastUpdateCheck(123)
	_ = s.SetTheme(ThemeDarkBlue)
	s2, _ := Open(p)
	if g := s2.Get(); g.CheckUpdates || g.LastUpdateCheck != 123 || g.Theme != ThemeDarkBlue {
		t.Fatalf("%+v", g)
	}
	if err := s2.Reset(); err != nil {
		t.Fatal(err)
	}
	if g := s2.Get(); !g.CheckUpdates || g.Theme != ThemeDarkBlack || g.LastUpdateCheck != 0 {
		t.Fatalf("%+v", g)
	}
}
