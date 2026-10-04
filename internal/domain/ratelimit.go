package domain

import "time"

// CheckRate enforces the sliding-window send rate (FR-10). Stub.
func CheckRate(r Rate, sent []Sent, now time.Time, runWrites, maxRun int) Decision {
	return Decision{}
}
