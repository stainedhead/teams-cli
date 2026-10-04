package usecasetest

import (
	"context"
	"testing"
	"time"
)

func TestClockSleepAdvances(t *testing.T) {
	c := &Clock{T: time.Unix(0, 0)}
	if err := c.Sleep(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	if !c.Now().Equal(time.Unix(1, 0)) {
		t.Fatalf("now = %v", c.Now())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Sleep(ctx, time.Second) == nil {
		t.Fatal("cancelled sleep must error")
	}
}
