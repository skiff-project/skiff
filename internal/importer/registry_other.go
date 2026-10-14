//go:build !windows

package importer

import "errors"

func winscpRegistryAvailable() bool { return false }
func puttyRegistryAvailable() bool  { return false }

func loadWinSCPRegistry() ([]Item, error) {
	return nil, errors.New("the Windows registry is only available on Windows")
}

func loadPuttyRegistry() ([]Item, error) {
	return nil, errors.New("the Windows registry is only available on Windows")
}
