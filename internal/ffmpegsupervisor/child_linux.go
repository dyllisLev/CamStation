package ffmpegsupervisor

import (
	"os/exec"
	"syscall"
)

// Kill ffmpeg even if go2rtc kills the wrapper without a catchable signal.
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
