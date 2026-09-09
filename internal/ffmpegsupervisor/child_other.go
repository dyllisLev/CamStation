//go:build !linux

package ffmpegsupervisor

import "os/exec"

func configureChild(cmd *exec.Cmd) {}
