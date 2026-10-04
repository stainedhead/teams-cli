package archtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func TestRepoImportGraph(t *testing.T) {
	for _, v := range CheckImports(repoRoot(t)) {
		t.Error(v)
	}
}

func TestRepoGoMod(t *testing.T) {
	for _, v := range CheckGoMod(filepath.Join(repoRoot(t), "go.mod")) {
		t.Error(v)
	}
}

func write(t *testing.T, root, name, src string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBadFixtureFails(t *testing.T) {
	cases := []struct {
		name, file, imp, want string
	}{
		{"domain imports usecase", "internal/domain/x.go", ModPath + "/internal/usecase", "outward"},
		{"domain imports core httpx", "internal/domain/x.go", CorePath + "/httpx", "core package"},
		{"usecase imports adapter", "internal/usecase/x.go", ModPath + "/internal/adapters/graph", "outward"},
		{"usecase imports infra", "internal/usecase/x.go", ModPath + "/internal/infra/clock", "outward"},
		{"usecase imports core auth", "internal/usecase/x.go", CorePath + "/auth", "core package"},
		{"domain imports yaml", "internal/domain/x.go", YAMLPath, "third-party"},
		{"infra imports domain", "internal/infra/clock/x.go", ModPath + "/internal/domain", "outward"},
		{"adapter sibling", "internal/adapters/cli/x.go", ModPath + "/internal/adapters/graph", "sibling"},
		{"adapter imports cmd", "internal/adapters/cli/x.go", ModPath + "/cmd/teams", "cmd"},
		{"okta daemon", "cmd/teams/x.go", "github.com/stainedhead/agent-okta-d/client", "agent-okta-d"},
		{"unknown third party", "cmd/teams/x.go", "github.com/other/lib", "third-party"},
	}
	for _, c := range cases {
		root := t.TempDir()
		dir := filepath.Dir(c.file)
		write(t, root, c.file, "package x\n\nimport _ \""+c.imp+"\"\n")
		_ = dir
		got := CheckImports(root)
		if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), c.want) {
			t.Errorf("%s: violations = %v, want one containing %q", c.name, got, c.want)
		}
	}
}

func TestGoodFixturePasses(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/domain/x.go", "package x\n\nimport (\n\t\"fmt\"\n\t_ \""+CorePath+"/output\"\n)\n\nvar _ = fmt.Sprint\n")
	write(t, root, "internal/adapters/cli/x.go", "package x\n\nimport _ \""+ModPath+"/internal/usecase\"\nimport _ \""+CorePath+"/docgen\"\n")
	write(t, root, "cmd/teams/x.go", "package main\n\nimport _ \""+ModPath+"/internal/adapters/graph\"\nimport _ \""+YAMLPath+"\"\n")
	if got := CheckImports(root); len(got) != 0 {
		t.Fatalf("unexpected violations: %v", got)
	}
}

func TestGoModRules(t *testing.T) {
	cases := []struct {
		name, mod string
		bad       bool
	}{
		{"ok", "module m\n\nrequire (\n\t" + YAMLPath + " v1.19.2\n\t" + CorePath + " v0.1.0\n)\n", false},
		{"replace", "module m\n\nreplace " + CorePath + " => ../core\n", true},
		{"extra module", "module m\n\nrequire github.com/foo/bar v1.0.0\n", true},
		{"okta", "module m\n\nrequire github.com/stainedhead/agent-okta-d v0.1.0\n", true},
		{"single line ok", "module m\n\nrequire " + CorePath + " v0.1.0\n", false},
	}
	for _, c := range cases {
		root := t.TempDir()
		write(t, root, "go.mod", c.mod)
		got := CheckGoMod(filepath.Join(root, "go.mod"))
		if (len(got) > 0) != c.bad {
			t.Errorf("%s: violations = %v", c.name, got)
		}
	}
}
