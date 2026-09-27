package media_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/internal/media"
)

func TestSystemClockSleep(t *testing.T) {
	var clock media.Clock = media.SystemClock{}
	start := clock.Now()
	if err := clock.Sleep(context.Background(), 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := clock.Now().Sub(start); elapsed < 5*time.Millisecond {
		t.Fatalf("slept %v, want >= 5ms", elapsed)
	}
	if err := clock.Sleep(context.Background(), 0); err != nil {
		t.Fatalf("zero sleep: %v", err)
	}
	if err := clock.Sleep(context.Background(), -time.Second); err != nil {
		t.Fatalf("negative sleep: %v", err)
	}
}

func TestSystemClockSleepCancellation(t *testing.T) {
	clock := media.SystemClock{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := clock.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if err := clock.Sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("zero sleep on cancelled context: got %v", err)
	}
	timeout, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	start := time.Now()
	if err := clock.Sleep(timeout, time.Hour); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("sleep ignored the deadline for %v", elapsed)
	}
}
