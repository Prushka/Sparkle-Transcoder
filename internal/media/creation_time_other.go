//go:build !windows && !linux && !darwin

package media

import (
	"os"
	"time"
)

func readFileCreationTime(_ string, _ os.FileInfo) (time.Time, bool) {
	return time.Time{}, false
}
