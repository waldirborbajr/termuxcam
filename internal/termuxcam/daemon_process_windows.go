package termuxcam

import (
	"os/exec"
	"syscall"
)

const detachedProcess uint32 = 0x00000008

func configureDaemonProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}
