//go:build linux

package priority

import "syscall"

const (
	prioProcess = 0
	lowPriority = 16
)

func LowPriority(pid int) error {
	return syscall.Setpriority(prioProcess, pid, lowPriority)
}
