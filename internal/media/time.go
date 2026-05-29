package media

import "time"

func unixNanos(n int64) time.Time {
	return time.Unix(0, n).UTC()
}
