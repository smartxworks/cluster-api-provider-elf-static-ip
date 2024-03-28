package util

import (
	"os"
	"time"

	"github.com/go-ping/ping"
)

func Ping(addr string) (bool, error) {
	pinger, err := ping.NewPinger(addr)
	if err != nil {
		return false, err
	}

	pinger.Count = 2
	pinger.Timeout = 50 * time.Millisecond
	pinger.SetPrivileged(true)
	// Blocks until finished.
	if err := pinger.Run(); err != nil {
		return false, err
	}
	if stats := pinger.Statistics(); stats.PacketsRecv >= 1 {
		return true, nil
	}

	return false, nil
}

// SkipPingIP returns true if need to skip ping related test cases.
// ping requires root privilege on MacOS. Add an env var SKIP_PING_IP to run tests easier on MacOS.
func SkipPingIP() bool {
	return os.Getenv("SKIP_PING_IP") != ""
}
