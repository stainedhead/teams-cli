package policyfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/infra/fstrust"
)

// ownerFS fakes the metadata of the policy file and its ancestors.
func ownerFS(path string, file fstrust.Info, dir fstrust.Info) *fstrust.Checker {
	lstat := func(p string) (fstrust.Info, error) {
		if p == path {
			return file, nil
		}
		return dir, nil
	}
	fstat := func(*os.File) (fstrust.Info, error) { return file, nil }
	return fstrust.New(fstrust.WithStat(lstat, fstat, 1000))
}

func writePolicy(t *testing.T) string {
	t.Helper()
	d, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(d, "teams.policy.yaml")
	if err := os.WriteFile(p, []byte(sample(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadTrustedPolicy(t *testing.T) {
	p := writePolicy(t)
	ch := ownerFS(p, fstrust.Info{UID: 0, Perm: 0o644, Kind: fstrust.KindRegular}, fstrust.Info{UID: 0, Perm: 0o755, Kind: fstrust.KindDir})
	pol, err := Load(p, withChecker(ch))
	if err != nil {
		t.Fatal(err)
	}
	if pol.Profile != "agent" {
		t.Fatal("not parsed")
	}
}

func TestLoadUntrustedPolicyExit9(t *testing.T) {
	p := writePolicy(t)
	dir := fstrust.Info{UID: 0, Perm: 0o755, Kind: fstrust.KindDir}
	for name, file := range map[string]fstrust.Info{
		"agent-owned":    {UID: 1000, Perm: 0o644, Kind: fstrust.KindRegular},
		"group-writable": {UID: 0, Perm: 0o664, Kind: fstrust.KindRegular},
		"world-writable": {UID: 0, Perm: 0o666, Kind: fstrust.KindRegular},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(p, withChecker(ownerFS(p, file, dir)))
			if err == nil || output.ExitOf(err) != 9 {
				t.Fatalf("want exit 9, got %v", err)
			}
			var de *domain.Error
			if !errors.As(err, &de) || !strings.Contains(de.Hint(), "install") {
				t.Fatalf("hint missing: %v", err)
			}
		})
	}
}

func TestLoadRealTempFileUntrusted(t *testing.T) {
	// With the real stat, a file the test user just wrote is agent-owned.
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	if _, err := Load(writePolicy(t)); err == nil || output.ExitOf(err) != 9 {
		t.Fatalf("want exit 9, got %v", err)
	}
}

func TestLoadSymlinkRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	p := writePolicy(t)
	link := filepath.Join(filepath.Dir(p), "link.yaml")
	if err := os.Symlink(p, link); err != nil {
		t.Skip("symlinks unsupported")
	}
	if _, err := Load(link); err == nil {
		t.Fatal("symlinked agent-owned policy must be refused")
	}
}

func TestLoadMissingTrustedPath(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || output.ExitOf(err) != 9 {
		t.Fatalf("got %v", err)
	}
}

func TestWithTrustedUIDsOption(t *testing.T) {
	p := writePolicy(t)
	var cfg loadConfig
	WithTrustedUIDs(7, 8)(&cfg)
	if len(cfg.trustedUIDs) != 2 {
		t.Fatalf("%v", cfg.trustedUIDs)
	}
	_ = p
}
