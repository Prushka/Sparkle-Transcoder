//go:build !darwin && !linux && !windows

package priority

func LowPriority(pid int) error {
	return nil
}
