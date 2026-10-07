//go:build windows

package scan

// New returns the scanner for the current platform.
func New() Scanner { return unsupported{} }

type unsupported struct{}

func (unsupported) Scan() ([]Listener, error) { return nil, ErrUnsupported }
