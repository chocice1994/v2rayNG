package session

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/anytls/sing-anytls/padding"
	"github.com/sagernet/sing/common/atomic"
	"github.com/sagernet/sing/common/logger"
)

// A generous relative budget avoids assuming an otherwise empty test runtime.
// The unpatched sleep-based worker retains all 64 goroutines for 30 seconds.
func TestClosedClientsReleaseCleanupWorkersPromptly(t *testing.T) {
	baseline := runtime.NumGoroutine()
	for i := 0; i < 64; i++ {
		var factory atomic.TypedValue[*padding.PaddingFactory]
		c := NewClient(context.Background(), logger.NOP(), nil, &factory, 30*time.Second, 30*time.Second, 0)
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseline+8 {
		if time.Now().After(deadline) {
			t.Fatalf("retired workers remain: before=%d after=%d", baseline, runtime.NumGoroutine())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
