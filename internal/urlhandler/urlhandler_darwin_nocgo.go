//go:build darwin && !cgo

package urlhandler

// Without cgo (the portable build, which has no Skiff.app), macOS cannot be
// asked.

func get() Status                            { return Status{} }
func register(exe string, icon []byte) error { return ErrUnsupported }
func remove(exe string) error                { return ErrUnsupported }
func openSettings() error                    { return ErrUnsupported }
