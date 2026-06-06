package dotenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBasic(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := `# a comment

DOTENV_PLAIN=bar
DOTENV_SPACED = qux
DOTENV_DQUOTE="hello world"
DOTENV_SQUOTE='single quoted'
DOTENV_EMPTY=
`
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Load(envFile); err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := map[string]string{
		"DOTENV_PLAIN":  "bar",
		"DOTENV_SPACED": "qux",
		"DOTENV_DQUOTE": "hello world",
		"DOTENV_SQUOTE": "single quoted",
		"DOTENV_EMPTY":  "",
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("DOTENV_EXISTING=fromfile\nDOTENV_NEW=fresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DOTENV_EXISTING", "preset")

	if err := Load(envFile); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if v := os.Getenv("DOTENV_EXISTING"); v != "preset" {
		t.Errorf("existing var overridden: got %q, want %q", v, "preset")
	}
	if v := os.Getenv("DOTENV_NEW"); v != "fresh" {
		t.Errorf("new var not set: got %q, want %q", v, "fresh")
	}
}

func TestLoadMissingFileErrors(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "nope.env")); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
