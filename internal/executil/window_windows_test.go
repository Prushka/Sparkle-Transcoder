package executil

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestHideWindowPreservesOtherProcessAttributes(t *testing.T) {
	cmd := exec.Command("unused.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200, NoInheritHandles: true}
	HideWindow(cmd)
	if !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags != 0x08000200 || !cmd.SysProcAttr.NoInheritHandles {
		t.Fatalf("unexpected process attributes: %+v", cmd.SysProcAttr)
	}
}
