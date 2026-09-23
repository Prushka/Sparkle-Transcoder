package executil

import (
	"os/exec"
	"syscall"
)

// HideWindow prevents console tools from opening a terminal when the backend
// itself was started by the Windows tray application without a console.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
