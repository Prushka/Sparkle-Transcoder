package lifecycle

import (
	"context"
	"os"

	"golang.org/x/sys/windows"
)

// WatchShutdown lets a console-free Windows host request the same graceful
// shutdown as a terminal signal, without exposing an HTTP shutdown endpoint.
func WatchShutdown(ctx context.Context, cancel context.CancelFunc) error {
	name := os.Getenv("SPARKLE_SHUTDOWN_EVENT")
	if name == "" {
		return nil
	}
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	handle, err := windows.OpenEvent(windows.SYNCHRONIZE, false, ptr)
	if err != nil {
		return err
	}
	go func() {
		defer windows.CloseHandle(handle)
		for ctx.Err() == nil {
			result, err := windows.WaitForSingleObject(handle, 250)
			if err != nil || result == windows.WAIT_OBJECT_0 {
				cancel()
				return
			}
		}
	}()
	return nil
}
