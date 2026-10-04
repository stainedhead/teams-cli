package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// maxTextInput bounds --file and stdin so a runaway pipe cannot exhaust
// memory. The policy's send.max_bytes is enforced by the use case; this is the
// hard ceiling on what the CLI will even read.
const maxTextInput = 4 << 20

var (
	errInputTooLarge = domain.NewValidation("message input exceeds the 4 MiB read limit", "shorten the message")
	errNotRegular    = domain.NewUsage("--file must be a regular file (no device, FIFO or directory)", "")
)

// readAll reads r up to maxTextInput bytes and fails when there is more (no
// silent truncation).
func readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxTextInput+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxTextInput {
		return nil, errInputTooLarge
	}
	return b, nil
}

// readInputFile is the default Deps.ReadFile: symlinks resolved, regular file
// only, handle re-checked after open, bounded read.
func readInputFile(path string) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(resolved); err != nil {
		return nil, err
	} else if !st.Mode().IsRegular() {
		return nil, errNotRegular
	}
	// O_NONBLOCK so a FIFO swapped in after the check cannot block open.
	f, err := os.OpenFile(resolved, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err != nil {
		return nil, err
	} else if !st.Mode().IsRegular() {
		return nil, errNotRegular
	}
	return readAll(f)
}

// listFlag is a repeatable, comma-separated string flag.
type listFlag struct{ v []string }

func (l *listFlag) String() string { return strings.Join(l.v, ",") }

func (l *listFlag) Set(s string) error {
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			l.v = append(l.v, p)
		}
	}
	return nil
}

func addList(fs *flag.FlagSet, name, usage string) *listFlag {
	l := &listFlag{}
	fs.Var(l, name, usage)
	return l
}

// parseAliases parses a list of --mention values.
func parseAliases(vals []string) ([]domain.Alias, error) {
	var out []domain.Alias
	for _, v := range vals {
		a, err := domain.ParseAlias(v)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// need checks the number of positional arguments.
func need(pos []string, n int, usage string) error {
	if len(pos) != n {
		return domain.NewUsage("expected: "+usage, "")
	}
	return nil
}

// textFlags registers --text and --file and tracks whether --text was given
// (an explicit empty --text is a different error from a missing one).
type textFlags struct {
	text, file string
	set        bool
}

func addText(fs *flag.FlagSet) *textFlags {
	t := &textFlags{}
	fs.Func("text", "message text", func(s string) error { t.text, t.set = s, true; return nil })
	fs.StringVar(&t.file, "file", "", "read the message from a file, or - for stdin")
	return t
}

// resolve returns the message text: exactly one of --text or --file.
func (t *textFlags) resolve(r *runner) (string, error) {
	switch {
	case t.set && t.file != "":
		return "", domain.NewUsage("use either --text or --file, not both", "")
	case t.set:
		return t.text, nil
	case t.file == "":
		return "", domain.NewUsage("one of --text or --file is required", "")
	case t.file == "-":
		if r.StdinIsTTY() {
			return "", domain.NewUsage("--file - needs piped input, but stdin is a terminal", "pipe the message in or use --text")
		}
		b, err := readAll(r.Stdin)
		return inputResult(b, err, "cannot read the message from stdin")
	}
	b, err := r.ReadFile(t.file)
	return inputResult(b, err, "cannot read --file")
}

func inputResult(b []byte, err error, msg string) (string, error) {
	if err != nil {
		if ce := asDomain(err); ce != nil {
			return "", ce
		}
		return "", domain.NewUsage(msg, "")
	}
	if len(b) > maxTextInput {
		return "", errInputTooLarge
	}
	return string(b), nil
}

func asDomain(err error) *domain.Error {
	var de *domain.Error
	if errors.As(err, &de) {
		return de
	}
	return nil
}

// parseWait accepts a Go duration ("30s", "2m") or plain seconds ("30").
func parseWait(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		n, nerr := strconv.Atoi(s)
		if nerr != nil {
			return 0, domain.NewUsage("--wait must be seconds or a duration such as 30s", "")
		}
		d = time.Duration(n) * time.Second
	}
	if d < 0 {
		return 0, domain.NewUsage("--wait must not be negative", "")
	}
	return d, nil
}
