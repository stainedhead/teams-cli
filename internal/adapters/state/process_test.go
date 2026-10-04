package state

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/infra/clock"
)

// The multi-process tests re-enter the test binary (os/exec) in helper mode.
const (
	envHelper = "TEAMS_STATE_HELPER"
	envDir    = "TEAMS_STATE_HELPER_DIR"
	envArg    = "TEAMS_STATE_HELPER_ARG"
)

func TestMain(m *testing.M) {
	switch os.Getenv(envHelper) {
	case "":
		os.Exit(m.Run())
	case "hold":
		helperHold()
	case "writer":
		helperWriter()
	case "cursor":
		helperCursor()
	}
	os.Exit(0)
}

func helperStore() *Store {
	s, err := Open(os.Getenv(envDir), clock.NewFake(t0), WithLockTimeout(20*time.Second))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	return s
}

// helperHold takes the state lock, announces it and holds it until stdin closes.
func helperHold() {
	s := helperStore()
	_ = s.locked(func() error {
		fmt.Println("locked")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return nil
	})
}

// helperWriter reserves and completes ARG keys named <pid>-<i>.
func helperWriter() {
	s := helperStore()
	n, _ := strconv.Atoi(os.Getenv(envArg))
	ctx := context.Background()
	for i := range n {
		key := fmt.Sprintf("k%d-%d", os.Getpid(), i)
		if _, err := s.Reserve(ctx, key, "h", "chat:dev", "t", t0); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		if err := s.Complete(ctx, key, "m"+key, t0); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(5)
		}
	}
}

func helperCursor() {
	s := helperStore()
	ctx := context.Background()
	if err := s.CacheChat(ctx, "user:jane.doe", "19:cached@unq.gbl.spaces"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(6)
	}
	if err := s.StoreDelta(ctx, "channel:sdlc-alerts", "delta-"+os.Getenv(envArg)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(7)
	}
}

func helperCmd(mode, dir, arg string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), envHelper+"="+mode, envDir+"="+dir, envArg+"="+arg)
	cmd.Stderr = os.Stderr
	return cmd
}

func TestLockExcludesOtherProcessAndTimesOutExit7(t *testing.T) {
	s, fc := newStore(t, WithLockTimeout(300*time.Millisecond))
	holder := helperCmd("hold", s.Dir(), "")
	in, _ := holder.StdinPipe()
	out, _ := holder.StdoutPipe()
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = in.Close()
			_ = holder.Wait()
		}
	}
	defer release()
	line, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("helper did not lock: %q", line)
	}

	start := time.Now()
	_, err := s.Reserve(context.Background(), "k", "h", "chat:dev", "", fc.Now())
	if err == nil || output.ExitOf(err) != 7 || !strings.Contains(err.Error(), "state busy") {
		t.Fatalf("want exit 7 state busy, got %v (exit %d)", err, output.ExitOf(err))
	}
	if d := time.Since(start); d < 250*time.Millisecond || d > 5*time.Second {
		t.Fatalf("timeout took %v", d)
	}
	if _, err := s.Get(context.Background(), "chat:dev"); output.ExitOf(err) != 7 {
		t.Fatalf("cursor read must also report busy, got %v", err)
	}
	if _, ok := s.ResolveChat(context.Background(), "chat:dev"); ok {
		t.Fatal("ResolveChat must fail open to a miss")
	}

	release()
	if _, err := s.Reserve(context.Background(), "k", "h", "chat:dev", "", fc.Now()); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestConcurrentProcessesDoNotLoseUpdates(t *testing.T) {
	s, _ := newStore(t)
	const procs, each = 4, 15
	var cmds []*exec.Cmd
	for range procs {
		c := helperCmd("writer", s.Dir(), strconv.Itoa(each))
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, c)
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("helper failed: %v", err)
		}
	}
	got, err := s.SentSince(context.Background(), t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != procs*each {
		t.Fatalf("lost updates: %d of %d sends recorded", len(got), procs*each)
	}
	lf, err := s.loadLedger()
	if err != nil || len(lf.Entries) != procs*each {
		t.Fatalf("ledger entries = %d, err %v", len(lf.Entries), err)
	}
}

func TestCursorStatePersistsAcrossProcesses(t *testing.T) {
	s, _ := newStore(t)
	for i := range 2 {
		if err := helperCmd("cursor", s.Dir(), strconv.Itoa(i)).Run(); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if id, ok := s.ResolveChat(ctx, "user:jane.doe"); !ok || id != "19:cached@unq.gbl.spaces" {
		t.Fatalf("chat cache = %q %v", id, ok)
	}
	cs, err := s.Get(ctx, "channel:sdlc-alerts")
	if err != nil || cs.DeltaToken != "delta-1" {
		t.Fatalf("delta = %q %v", cs.DeltaToken, err)
	}
}

func TestInProcessGoroutinesSameKeyOnePost(t *testing.T) {
	s, fc := newStore(t)
	const n = 8
	type result struct {
		out domain.ReserveOutcome
		err error
	}
	res := make(chan result, n)
	for range n {
		go func() {
			r, err := s.Reserve(context.Background(), "same", "h", "chat:dev", "", fc.Now())
			res <- result{r.Outcome, err}
		}()
	}
	news, conflicts := 0, 0
	for range n {
		r := <-res
		switch {
		case r.err == nil && r.out == domain.ReserveNew:
			news++
		case r.err != nil && output.ExitOf(r.err) == 7:
			conflicts++
		default:
			t.Fatalf("unexpected: %+v", r)
		}
	}
	if news != 1 || conflicts != n-1 {
		t.Fatalf("news=%d conflicts=%d", news, conflicts)
	}
}
