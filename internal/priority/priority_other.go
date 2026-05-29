//go:build !darwin && !linux

package priority

func LowPriority(pid int) error {
	return nil
}
