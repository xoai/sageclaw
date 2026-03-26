//go:build !windows

package rpc

import (
	"os"
	"syscall"
)

func sendHUPSignal(p *os.Process) {
	p.Signal(syscall.SIGHUP)
}
