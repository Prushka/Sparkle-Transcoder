//go:build windows

package media

import (
	"os"
	"syscall"
	"time"
)

func readFileCreationTime(_ string, info os.FileInfo) (time.Time, bool) {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return time.Time{}, false
	}
	if data.CreationTime.LowDateTime == 0 && data.CreationTime.HighDateTime == 0 {
		return time.Time{}, false
	}
	nanos := data.CreationTime.Nanoseconds()
	return time.Unix(0, nanos).UTC(), true
}
