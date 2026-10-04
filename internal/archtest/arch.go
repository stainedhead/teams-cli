// Package archtest enforces the Clean Architecture dependency rule and the
// dependency allowlist. The checks live in non-test code so tests can run them
// against deliberately bad fixture trees.
package archtest

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Module paths.
const (
	ModPath  = "github.com/stainedhead/teams-cli"
	CorePath = "github.com/stainedhead/agent-cli-core"
	YAMLPath = "github.com/goccy/go-yaml"
	// OktaPath is the credential daemon module. Production code reaches it
	// only through core's auth/oktad adapter; the one direct import allowed is
	// the clienttest fake daemon, from cmd/teams tests.
	OktaPath = "github.com/stainedhead/agent-okta-d"
	// OktaFake is the fake daemon package cmd/teams tests may import.
	OktaFake = OktaPath + "/pkg/client/clienttest"
)

// isOktaFakeUse reports whether file may import imp: the clienttest fake from
// a _test.go file directly in cmd/teams.
func isOktaFakeUse(root, file, imp string) bool {
	r := filepath.ToSlash(rel(root, file))
	return imp == OktaFake && strings.HasPrefix(r, "cmd/teams/") && strings.HasSuffix(r, "_test.go")
}

type layer struct {
	dir        string
	internalOK []string
	coreOK     []string
	thirdParty []string
}

var adapterCore = []string{"output", "auth", "httpx", "audit", "selftest", "docgen"}

var layers = []layer{
	{dir: "internal/domain", internalOK: []string{"internal/domain"}, coreOK: []string{"output"}},
	{dir: "internal/usecase", internalOK: []string{"internal/domain", "internal/usecase"}, coreOK: []string{"output"}},
	{dir: "internal/infra", internalOK: []string{"internal/infra"}},
	{dir: "internal/adapters", internalOK: []string{"internal/domain", "internal/usecase", "internal/infra", "internal/adapters"},
		coreOK: adapterCore, thirdParty: []string{YAMLPath}},
	{dir: "cmd/teams", internalOK: []string{"internal"}, coreOK: adapterCore, thirdParty: []string{YAMLPath}},
}

// CheckImports walks root and returns import-graph violations.
func CheckImports(root string) []string {
	var out []string
	for _, l := range layers {
		walkGo(root, filepath.Join(root, l.dir), func(file string, imports []string) {
			for _, imp := range imports {
				if isOktaFakeUse(root, file, imp) {
					continue
				}
				if msg := violation(l, imp); msg != "" {
					out = append(out, fmt.Sprintf("%s imports %s: %s", rel(root, file), imp, msg))
				}
			}
		})
	}
	// Adapters must not import each other (they meet in cmd/teams).
	base := filepath.Join(root, "internal/adapters")
	entries, _ := os.ReadDir(base)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		self := ModPath + "/internal/adapters/" + e.Name()
		walkGo(root, filepath.Join(base, e.Name()), func(file string, imports []string) {
			for _, imp := range imports {
				if strings.HasPrefix(imp, ModPath+"/internal/adapters/") && imp != self && !strings.HasPrefix(imp, self+"/") {
					out = append(out, fmt.Sprintf("%s imports sibling adapter %s", rel(root, file), imp))
				}
			}
		})
	}
	// Nothing under internal imports cmd; agent-okta-d is imported only by the clienttest rule.
	walkGo(root, root, func(file string, imports []string) {
		for _, imp := range imports {
			if strings.Contains(imp, "agent-okta-d") && !isOktaFakeUse(root, file, imp) {
				out = append(out, fmt.Sprintf("%s imports %s: use core's auth/oktad; only cmd/teams tests may import clienttest", rel(root, file), imp))
			}
			if strings.HasPrefix(rel(root, file), "internal"+string(filepath.Separator)) && strings.HasPrefix(imp, ModPath+"/cmd") {
				out = append(out, fmt.Sprintf("%s imports %s: internal packages must not import cmd", rel(root, file), imp))
			}
		}
	})
	sort.Strings(out)
	return out
}

func violation(l layer, imp string) string {
	switch {
	case strings.HasPrefix(imp, ModPath+"/"):
		r := strings.TrimPrefix(imp, ModPath+"/")
		for _, ok := range l.internalOK {
			if r == ok || strings.HasPrefix(r, ok+"/") {
				return ""
			}
		}
		return "outward or sideways dependency"
	case imp == CorePath || strings.HasPrefix(imp, CorePath+"/"):
		r := strings.TrimPrefix(strings.TrimPrefix(imp, CorePath), "/")
		for _, ok := range l.coreOK {
			if r == ok || strings.HasPrefix(r, ok+"/") {
				return ""
			}
		}
		return "core package not allowed in this layer"
	case isStdlib(imp):
		return ""
	}
	for _, tp := range l.thirdParty {
		if imp == tp || strings.HasPrefix(imp, tp+"/") {
			return ""
		}
	}
	return "third-party package not allowed in this layer"
}

// CheckGoMod validates go.mod: allowed requires only, no replace.
func CheckGoMod(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{err.Error()}
	}
	defer func() { _ = f.Close() }()
	var out []string
	inRequire := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "replace"):
			out = append(out, "go.mod has a replace directive: "+line)
		case line == "require (":
			inRequire = true
		case line == ")":
			inRequire = false
		case inRequire || strings.HasPrefix(line, "require "):
			mod := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(mod) == 0 {
				continue
			}
			// agent-okta-d is required because core's oktad adapter depends
			// on it (and tests use its clienttest); imports are policed above.
			if mod[0] != CorePath && mod[0] != YAMLPath && mod[0] != OktaPath {
				out = append(out, "go.mod requires a module outside the allowlist: "+mod[0])
			}
		}
	}
	return out
}

func isStdlib(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".")
}

func walkGo(root, dir string, fn func(file string, imports []string)) {
	if _, err := os.Stat(dir); err != nil {
		return
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".worktrees", "dist", "bin", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		var imps []string
		for _, s := range f.Imports {
			imps = append(imps, strings.Trim(s.Path.Value, `"`))
		}
		fn(p, imps)
		return nil
	})
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}
