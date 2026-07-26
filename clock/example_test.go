package clock_test

import (
	"context"
	"fmt"
	"time"

	clockfake "github.com/primandproper/platform-go/v7/clock/fake"
)

// Example demonstrates driving time-dependent code deterministically with the
// fake clock: a goroutine sleeps for an hour of clock time, and the test
// advances the clock instead of waiting.
func Example() {
	fc := clockfake.New(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))

	done := make(chan error, 1)
	go func() {
		done <- fc.Sleep(context.Background(), time.Hour)
	}()

	// Wait for the sleeper to register, then advance past its deadline.
	fc.BlockUntil(1)
	fc.Advance(time.Hour)

	if err := <-done; err != nil {
		fmt.Println("sleep failed:", err)
		return
	}

	fmt.Println(fc.Now().UTC().Format(time.RFC3339))
	// Output: 2026-01-01T01:00:00Z
}
