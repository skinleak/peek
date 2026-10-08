//go:build !linux

package kill

// isZombie is only implemented on Linux; elsewhere peek relies on signal 0.
func isZombie(int) bool { return false }
