package main

import (
	"os"
	"runtime"
	"syscall"
)

func protectParent() {
	runtime.LockOSThread()
	parent := os.Getppid()
	_, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, 1, uintptr(syscall.SIGTERM), 0, 0, 0, 0)
	if errno != 0 || os.Getppid() != parent {
		os.Exit(130)
	}
}
