//go:build windows

package rpc

import "os"

func sendHUPSignal(_ *os.Process) {
	// SIGHUP is not available on Windows. The skill reload will happen
	// on the next periodic check or manual reload.
}
