package meshtest

import (
	"testing"
	"time"
)

// Deadline is when a test's waits give up: the test binary's deadline (go
// test -timeout) less a margin for cleanup and the failure's report, or ten
// minutes from now when there is none. A wait bounded by it fails only when
// what it waits for never comes, not when a loaded box is slow to bring it.
func Deadline(t *testing.T) time.Time {
	end, ok := t.Deadline()
	if !ok {
		return time.Now().Add(10 * time.Minute)
	}
	return end.Add(-min(30*time.Second, time.Until(end)/4))
}

// Within is the time left until Deadline, for a select on time.After.
func Within(t *testing.T) time.Duration {
	return time.Until(Deadline(t))
}

// Until polls cond every 20ms until it holds, failing t at Deadline.
func Until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := Deadline(t); !cond(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}
