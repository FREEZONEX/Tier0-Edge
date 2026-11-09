package mcpserver

import "syscall"

func applySandboxLimits() error {
	for _, limit := range []struct {
		resource int
		value    uint64
	}{{syscall.RLIMIT_CPU, 120}, {syscall.RLIMIT_CORE, 0}, {syscall.RLIMIT_FSIZE, 0}, {syscall.RLIMIT_NOFILE, 32}} {
		var current syscall.Rlimit
		if err := syscall.Getrlimit(limit.resource, &current); err != nil {
			return err
		}
		value := limit.value
		if current.Max < value {
			value = current.Max
		}
		if err := syscall.Setrlimit(limit.resource, &syscall.Rlimit{Cur: value, Max: value}); err != nil {
			return err
		}
	}
	return nil
}
func processRSS(int) (uint64, bool) { return 0, false }
