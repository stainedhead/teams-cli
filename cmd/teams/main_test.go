package main

import "testing"

func TestRunSkeletonExitsZero(t *testing.T) {
	if got := run(nil); got != 0 {
		t.Fatalf("run = %d", got)
	}
}
