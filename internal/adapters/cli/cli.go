package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/selftest"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// BuildInfo is the version metadata stamped with -ldflags (REL-4).
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Deps is everything the CLI needs from the composition root. Only
// NewCommands and Selftest are lazy: version, skill and help never build the
// object graph, so they work without a policy, credentials or a daemon.
type Deps struct {
	// NewCommands builds the use-case facade (policy load, state, Graph).
	NewCommands func(ctx context.Context) (usecase.Commands, error)
	// Selftest runs the allow/deny matrix (core selftest runner). Nil means
	// the command reports that selftest is unavailable.
	Selftest func(ctx context.Context, readOnly bool) (selftest.Result, error)
	Build    BuildInfo
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	// ReadFile reads --file; defaults to a bounded, regular-file-only reader.
	ReadFile func(path string) ([]byte, error)
	// StdinIsTTY reports whether stdin is an interactive terminal, in which
	// case `--file -` is refused. Defaults to a check on os.Stdin.
	StdinIsTTY func() bool
}

// globals are the flags every command accepts (FR-29).
type globals struct {
	format   string
	maxBytes int
	offset   int
}

// handler runs a parsed command and returns the envelope data.
type handler func(ctx context.Context, c usecase.Commands, pos []string) (any, error)

// command is one row of the command table. The same table drives dispatch,
// help and the generated skill document, so they cannot drift.
type command struct {
	name        string // "destinations list"
	description string
	usage       string
	examples    []string
	forbidden   []string
	hidden      bool // not in the generated skill
	local       bool // does not need the use-case facade
	// build declares flags on fs and returns the handler.
	build func(r *runner, fs *flag.FlagSet) handler
}

type runner struct {
	Deps
	g globals
}

// Run executes one CLI invocation and returns the process exit code. args
// excludes the program name.
func Run(ctx context.Context, args []string, d Deps) output.ExitCode {
	if d.Stdout == nil {
		d.Stdout = io.Discard
	}
	if d.Stderr == nil {
		d.Stderr = io.Discard
	}
	if d.Stdin == nil {
		d.Stdin = strings.NewReader("")
	}
	if d.ReadFile == nil {
		d.ReadFile = readInputFile
	}
	if d.StdinIsTTY == nil {
		d.StdinIsTTY = func() bool { return isTerminal(d.Stdin) }
	}
	r := &runner{Deps: d}
	return r.run(ctx, args)
}

// isTerminal reports whether in is a character device (an interactive tty).
func isTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func (r *runner) run(ctx context.Context, args []string) output.ExitCode {
	cmd, rest, err := lookup(args)
	if err != nil {
		return r.fail(err)
	}
	if cmd == nil { // bare "teams", or "teams help"
		return r.emit(helpData())
	}
	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&r.g.format, "format", "json", "output format: json|table|text")
	fs.IntVar(&r.g.maxBytes, "max-bytes", 0, "bound on output size in bytes")
	fs.IntVar(&r.g.offset, "offset", 0, "resume offset from a truncated output")
	h := cmd.build(r, fs)
	pos, err := parseInterleaved(fs, rest)
	if errors.Is(err, flag.ErrHelp) {
		return r.emit(obj{"command": cmd.name, "usage": cmd.usage, "examples": cmd.examples})
	}
	if err != nil {
		return r.fail(err)
	}
	var c usecase.Commands
	if !cmd.local {
		if r.NewCommands == nil {
			return r.fail(domain.NewValidation("teams is not wired to a Teams backend", ""))
		}
		if c, err = r.NewCommands(ctx); err != nil {
			return r.fail(err)
		}
	}
	data, err := h(ctx, c, pos)
	if err != nil {
		return r.fail(err)
	}
	if env, ok := data.(envResult); ok { // ready-made envelope (selftest)
		return r.write(output.Envelope(env))
	}
	if raw, ok := data.(rawText); ok { // skill document: Markdown, not an envelope
		_, _ = io.WriteString(r.Stdout, string(raw))
		return output.ExitOK
	}
	return r.emit(data)
}

// envResult carries a ready-made envelope from a handler.
type envResult output.Envelope

// rawText marks output written verbatim (the skill document).
type rawText string

func (r *runner) opts() output.Options {
	return output.Options{
		Format: output.Format(r.g.format),
		Bounds: output.Bounds{MaxBytes: r.g.maxBytes, Offset: r.g.offset},
	}
}

func (r *runner) emit(data any) output.ExitCode {
	return r.write(output.Success(data, nil))
}

func (r *runner) fail(err error) output.ExitCode {
	return r.write(output.FromError(err))
}

func (r *runner) write(env output.Envelope) output.ExitCode {
	if err := output.Write(r.Stdout, env, r.opts()); err != nil {
		// Bad --format/--max-bytes/--offset: report as a failure envelope.
		fe := output.FromError(err)
		if werr := output.Write(r.Stdout, fe, output.Options{}); werr != nil {
			_, _ = fmt.Fprintln(r.Stderr, werr)
		}
		return fe.ExitCode()
	}
	return env.ExitCode()
}

// lookup finds the command for args and returns the remaining arguments. A nil
// command with a nil error means "show help".
func lookup(args []string) (*command, []string, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		return nil, nil, nil
	}
	table := commands()
	if len(args) >= 2 {
		name := args[0] + " " + args[1]
		for i := range table {
			if table[i].name == name {
				return &table[i], args[2:], nil
			}
		}
	}
	for i := range table {
		if table[i].name == args[0] {
			return &table[i], args[1:], nil
		}
	}
	var sub []string
	for i := range table {
		if strings.HasPrefix(table[i].name, args[0]+" ") {
			sub = append(sub, strings.TrimPrefix(table[i].name, args[0]+" "))
		}
	}
	if len(sub) > 0 {
		sort.Strings(sub)
		return nil, nil, domain.NewUsage(fmt.Sprintf("unknown or missing subcommand for %q", args[0]),
			"expected one of: "+strings.Join(sub, ", "))
	}
	return nil, nil, domain.NewUsage(fmt.Sprintf("unknown command %q", args[0]), "run `teams help` for the command list")
}

func helpData() obj {
	var usages []string
	for _, c := range commands() {
		if !c.hidden {
			usages = append(usages, c.usage)
		}
	}
	return obj{"commands": usages}
}

// parseInterleaved parses flags that may appear before, between or after
// positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, domain.NewUsage(err.Error(), "run `teams help` for the command list")
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}
