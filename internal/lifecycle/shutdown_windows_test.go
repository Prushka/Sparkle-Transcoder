package lifecycle

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestManagedShutdownEventCancelsContext(t *testing.T) {
	name := fmt.Sprintf(`Local\Sparkle.Shutdown.Test.%d.%d`, os.Getpid(), time.Now().UnixNano())
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	event, err := windows.CreateEvent(nil, 1, 0, ptr)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(event)
	t.Setenv("SPARKLE_SHUTDOWN_EVENT", name)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := WatchShutdown(ctx, cancel); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("backend stopped before the host requested shutdown")
	}
	if err := windows.SetEvent(event); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("backend ignored the host shutdown event")
	}
}

func TestUnmanagedLaunchDoesNotRequestShutdown(t *testing.T) {
	t.Setenv("SPARKLE_SHUTDOWN_EVENT", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := WatchShutdown(ctx, cancel); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("ordinary terminal launch was canceled")
	}
}

func TestMissingManagedShutdownEventReturnsError(t *testing.T) {
	t.Setenv("SPARKLE_SHUTDOWN_EVENT", fmt.Sprintf(`Local\Sparkle.Missing.%d.%d`, os.Getpid(), time.Now().UnixNano()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := WatchShutdown(ctx, cancel); err == nil {
		t.Fatal("expected a missing event to fail explicitly")
	}
}
