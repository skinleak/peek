package kill

import (
	"os"
	"strconv"
	"strings"
)

// isZombie reports whether pid has exited but not been reaped by its parent.
// A zombie still accepts signal 0, so without this check a process that
// exited would look alive. That happens when the parent is a container's
// PID 1 that doesn't reap orphans.
func isZombie(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// The state follows the command name, which may itself contain ')'.
	_, rest, ok := strings.Cut(string(b[strings.LastIndexByte(string(b), ')')+1:]), " ")
	return ok && strings.HasPrefix(rest, "Z")
}
