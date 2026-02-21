package container

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLocal_GoProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/foo\n\ngo 1.23\n")

	langs := Detect(context.Background(), dir)
	if len(langs) != 1 {
		t.Fatalf("expected 1 language, got %d: %v", len(langs), langs)
	}
	if langs[0].Lang != LangGo {
		t.Errorf("expected Go, got %s", langs[0].Lang)
	}
	if langs[0].Version != "1.23" {
		t.Errorf("expected version 1.23, got %q", langs[0].Version)
	}
}

func TestDetectLocal_MultiLanguage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module foo\n\ngo 1.22\n")
	writeFile(t, dir, "package.json", `{"engines":{"node":">=20"}}`)
	writeFile(t, dir, "Gemfile", `source "https://rubygems.org"\nruby "3.3.0"\n`)

	langs := Detect(context.Background(), dir)
	if len(langs) != 3 {
		t.Fatalf("expected 3 languages, got %d: %v", len(langs), langs)
	}
	// Should be sorted: Go, Node, Ruby
	if langs[0].Lang != LangGo {
		t.Errorf("expected Go first, got %s", langs[0].Lang)
	}
	if langs[1].Lang != LangNode {
		t.Errorf("expected Node second, got %s", langs[1].Lang)
	}
	if langs[2].Lang != LangRuby {
		t.Errorf("expected Ruby third, got %s", langs[2].Lang)
	}
}

func TestDetectLocal_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	langs := Detect(context.Background(), dir)
	if len(langs) != 0 {
		t.Errorf("expected 0 languages for empty repo, got %d: %v", len(langs), langs)
	}
}

func TestDetectLocal_PythonVariants(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"requirements.txt", "requirements.txt"},
		{"pyproject.toml", "pyproject.toml"},
		{"setup.py", "setup.py"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tt.file, "# python project\n")

			langs := Detect(context.Background(), dir)
			if len(langs) != 1 {
				t.Fatalf("expected 1 language, got %d", len(langs))
			}
			if langs[0].Lang != LangPython {
				t.Errorf("expected Python, got %s", langs[0].Lang)
			}
		})
	}
}

func TestDetectLocal_PythonDedup(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", "flask\n")
	writeFile(t, dir, "pyproject.toml", "[project]\n")
	writeFile(t, dir, "setup.py", "from setuptools import setup\n")

	langs := Detect(context.Background(), dir)
	if len(langs) != 1 {
		t.Fatalf("expected 1 language (deduped Python), got %d: %v", len(langs), langs)
	}
	if langs[0].Lang != LangPython {
		t.Errorf("expected Python, got %s", langs[0].Lang)
	}
}

func TestDetectLocal_JavaVariants(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"pom.xml", "pom.xml"},
		{"build.gradle", "build.gradle"},
		{"build.gradle.kts", "build.gradle.kts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tt.file, "// java project\n")

			langs := Detect(context.Background(), dir)
			if len(langs) != 1 {
				t.Fatalf("expected 1 language, got %d", len(langs))
			}
			if langs[0].Lang != LangJava {
				t.Errorf("expected Java, got %s", langs[0].Lang)
			}
		})
	}
}

func TestParseGoVersion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"standard", "module foo\n\ngo 1.23\n", "1.23"},
		{"with patch", "module foo\n\ngo 1.23.4\n", "1.23"},
		{"toolchain line", "module foo\n\ngo 1.22\n\ntoolchain go1.23.0\n", "1.22"},
		{"no go directive", "module foo\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "go.mod", tt.content)
			got := parseGoVersion(dir)
			if got != tt.want {
				t.Errorf("parseGoVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseNodeVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  ".node-version",
			files: map[string]string{".node-version": "20.11.0\n"},
			want:  "20",
		},
		{
			name:  ".nvmrc",
			files: map[string]string{".nvmrc": "v18.17.0\n"},
			want:  "18",
		},
		{
			name:  "package.json engines",
			files: map[string]string{"package.json": `{"engines":{"node":">=20"}}`},
			want:  "20",
		},
		{
			name:  ".node-version takes priority over .nvmrc",
			files: map[string]string{".node-version": "22\n", ".nvmrc": "20\n"},
			want:  "22",
		},
		{
			name:  "no version files",
			files: map[string]string{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, c := range tt.files {
				writeFile(t, dir, f, c)
			}
			got := parseNodeVersion(dir)
			if got != tt.want {
				t.Errorf("parseNodeVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRubyVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  ".ruby-version",
			files: map[string]string{".ruby-version": "3.3.0\n"},
			want:  "3.3.0",
		},
		{
			name:  ".ruby-version with prefix",
			files: map[string]string{".ruby-version": "ruby-3.2.1\n"},
			want:  "3.2.1",
		},
		{
			name:  "Gemfile ruby directive",
			files: map[string]string{"Gemfile": "source \"https://rubygems.org\"\nruby \"3.3.0\"\n"},
			want:  "3.3.0",
		},
		{
			name:  ".ruby-version takes priority",
			files: map[string]string{".ruby-version": "3.2.0\n", "Gemfile": "ruby \"3.3.0\"\n"},
			want:  "3.2.0",
		},
		{
			name:  "no version files",
			files: map[string]string{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, c := range tt.files {
				writeFile(t, dir, f, c)
			}
			got := parseRubyVersion(dir)
			if got != tt.want {
				t.Errorf("parseRubyVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsePythonVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  ".python-version",
			files: map[string]string{".python-version": "3.12.1\n"},
			want:  "3.12",
		},
		{
			name:  "pyproject.toml requires-python",
			files: map[string]string{"pyproject.toml": "[project]\nrequires-python = \">=3.11\"\n"},
			want:  "3.11",
		},
		{
			name:  ".python-version takes priority",
			files: map[string]string{".python-version": "3.12\n", "pyproject.toml": "requires-python = \">=3.11\"\n"},
			want:  "3.12",
		},
		{
			name:  "no version files",
			files: map[string]string{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, c := range tt.files {
				writeFile(t, dir, f, c)
			}
			got := parsePythonVersion(dir)
			if got != tt.want {
				t.Errorf("parsePythonVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRustVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "rust-toolchain.toml",
			files: map[string]string{"rust-toolchain.toml": "[toolchain]\nchannel = \"1.77.0\"\n"},
			want:  "1.77.0",
		},
		{
			name:  "rust-toolchain file",
			files: map[string]string{"rust-toolchain": "1.76.0\n"},
			want:  "1.76.0",
		},
		{
			name:  "toml takes priority",
			files: map[string]string{"rust-toolchain.toml": "[toolchain]\nchannel = \"1.77.0\"\n", "rust-toolchain": "1.76.0\n"},
			want:  "1.77.0",
		},
		{
			name:  "no version files",
			files: map[string]string{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, c := range tt.files {
				writeFile(t, dir, f, c)
			}
			got := parseRustVersion(dir)
			if got != tt.want {
				t.Errorf("parseRustVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseJavaVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  ".java-version",
			files: map[string]string{".java-version": "21\n"},
			want:  "21",
		},
		{
			name:  ".java-version with minor",
			files: map[string]string{".java-version": "17.0.2\n"},
			want:  "17",
		},
		{
			name:  "no version file",
			files: map[string]string{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, c := range tt.files {
				writeFile(t, dir, f, c)
			}
			got := parseJavaVersion(dir)
			if got != tt.want {
				t.Errorf("parseJavaVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectRemote(t *testing.T) {
	orig := ghCommandFunc
	defer func() { ghCommandFunc = orig }()

	ghCommandFunc = func(_ context.Context, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[1] == "repos/owner/repo/languages" {
			return json.Marshal(map[string]int64{"Go": 50000, "TypeScript": 30000})
		}
		// Return go.mod for version detection
		if len(args) >= 2 && args[1] == "repos/owner/repo/contents/go.mod" {
			content := base64.StdEncoding.EncodeToString([]byte("module foo\n\ngo 1.23\n"))
			return json.Marshal(map[string]string{"content": content, "encoding": "base64"})
		}
		return nil, fmt.Errorf("not found")
	}

	langs := Detect(context.Background(), "owner/repo")
	if len(langs) != 2 {
		t.Fatalf("expected 2 languages, got %d: %v", len(langs), langs)
	}
	if langs[0].Lang != LangGo {
		t.Errorf("expected Go first, got %s", langs[0].Lang)
	}
	if langs[0].Version != "1.23" {
		t.Errorf("expected Go version 1.23, got %q", langs[0].Version)
	}
	if langs[1].Lang != LangNode {
		t.Errorf("expected Node second, got %s", langs[1].Lang)
	}
}

func TestDetectRemote_APIFailure(t *testing.T) {
	orig := ghCommandFunc
	defer func() { ghCommandFunc = orig }()

	ghCommandFunc = func(_ context.Context, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("API error")
	}

	langs := Detect(context.Background(), "owner/repo")
	if len(langs) != 0 {
		t.Errorf("expected 0 languages on API failure, got %d", len(langs))
	}
}

func TestGitHubLanguageMapping(t *testing.T) {
	tests := []struct {
		ghName string
		want   Language
		ok     bool
	}{
		{"Go", LangGo, true},
		{"JavaScript", LangNode, true},
		{"TypeScript", LangNode, true},
		{"Python", LangPython, true},
		{"Ruby", LangRuby, true},
		{"Rust", LangRust, true},
		{"Java", LangJava, true},
		{"Kotlin", LangJava, true},
		{"PHP", LangPHP, true},
		{"Haskell", "", false},
		{"Shell", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.ghName, func(t *testing.T) {
			lang, ok := ghLanguageMap[tt.ghName]
			if ok != tt.ok {
				t.Errorf("ghLanguageMap[%q] ok = %v, want %v", tt.ghName, ok, tt.ok)
			}
			if ok && lang != tt.want {
				t.Errorf("ghLanguageMap[%q] = %s, want %s", tt.ghName, lang, tt.want)
			}
		})
	}
}

func TestVersionFallback(t *testing.T) {
	// When version files are missing/unparseable, version should be empty
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module foo\n") // No go directive
	writeFile(t, dir, "package.json", `{}`)     // No engines

	langs := Detect(context.Background(), dir)
	if len(langs) != 2 {
		t.Fatalf("expected 2 languages, got %d: %v", len(langs), langs)
	}
	for _, l := range langs {
		if l.Version != "" {
			t.Errorf("expected empty version for %s when unparseable, got %q", l.Lang, l.Version)
		}
	}
}

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/absolute/path", true},
		{"./relative/path", true},
		{"../parent/path", true},
		{"owner/repo", false},
		{"my-org/my-repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isLocalPath(tt.path); got != tt.want {
				t.Errorf("isLocalPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestSortDetected(t *testing.T) {
	langs := []DetectedLang{
		{Lang: LangRuby},
		{Lang: LangGo},
		{Lang: LangNode},
	}
	sortDetected(langs)
	if langs[0].Lang != LangGo || langs[1].Lang != LangNode || langs[2].Lang != LangRuby {
		t.Errorf("unexpected sort order: %v", langs)
	}
}

// writeFile is a test helper that creates a file with the given content.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
