package domain

import (
	"fmt"
	"sort"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
)

// Rule ids for send limits.
const (
	RuleRateRun    = "rate.run_cap"
	RuleRateMinute = "rate.per_minute"
	RuleRateHour   = "rate.per_hour"
)

// CheckRate enforces the per-run write cap and the minute and hour sliding
// windows over sent history (FR-10). Windows are (now-w, now]: a send exactly
// w old no longer counts. Limits <= 0 are disabled. The denial carries
// RetryAfter, the time until a slot frees.
func CheckRate(r Rate, sent []Sent, now time.Time, runWrites, maxRun int) Decision {
	if maxRun > 0 && runWrites >= maxRun {
		d := deny(RuleRateRun, fmt.Sprintf("per-run write cap of %d reached", maxRun))
		return d
	}
	var worst Decision
	for _, w := range []struct {
		rule   string
		limit  int
		window time.Duration
		name   string
	}{
		{RuleRateMinute, r.PerMinute, time.Minute, "minute"},
		{RuleRateHour, r.PerHour, time.Hour, "hour"},
	} {
		if w.limit <= 0 {
			continue
		}
		wait, over := windowWait(sent, now, w.window, w.limit)
		if over && wait >= worst.RetryAfter {
			worst = deny(w.rule, fmt.Sprintf("send rate limit of %d per %s reached; retry after %s", w.limit, w.name, wait.Round(time.Second)))
			worst.RetryAfter = wait
		}
	}
	if worst.RuleID != "" {
		return worst
	}
	return Decision{Allowed: true, Category: output.CategoryOK}
}

// windowWait reports whether the window is full and how long until the
// (count-limit+1)th newest in-window send expires.
func windowWait(sent []Sent, now time.Time, window time.Duration, limit int) (time.Duration, bool) {
	cutoff := now.Add(-window)
	var in []time.Time
	for _, s := range sent {
		if s.At.After(cutoff) && !s.At.After(now) {
			in = append(in, s.At)
		}
	}
	if len(in) < limit {
		return 0, false
	}
	sortTimes(in)
	oldestBlocking := in[len(in)-limit]
	wait := oldestBlocking.Add(window).Sub(now)
	if wait < 0 {
		wait = 0
	}
	return wait, true
}

func sortTimes(ts []time.Time) {
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
}
