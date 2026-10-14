//go:build windows

package importer

import (
	"strconv"

	"golang.org/x/sys/windows/registry"
)

const (
	winscpKey = `Software\Martin Prikryl\WinSCP 2\Sessions`
	puttyKey  = `Software\SimonTatham\PuTTY\Sessions`
)

func keyAvailable(path string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

func winscpRegistryAvailable() bool { return keyAvailable(winscpKey) }
func puttyRegistryAvailable() bool  { return keyAvailable(puttyKey) }

// readSessions loads every subkey of path as a map of string values.
func readSessions(path string) (map[string]map[string]string, []string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		return nil, nil, err
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]map[string]string{}
	for _, n := range names {
		sk, err := registry.OpenKey(k, n, registry.READ)
		if err != nil {
			continue
		}
		vals, _ := sk.ReadValueNames(-1)
		kv := map[string]string{}
		for _, v := range vals {
			if s, _, err := sk.GetStringValue(v); err == nil {
				kv[v] = s
			} else if i, _, err := sk.GetIntegerValue(v); err == nil {
				kv[v] = strconv.FormatUint(i, 10)
			}
		}
		sk.Close()
		out[n] = kv
	}
	return out, names, nil
}

func loadWinSCPRegistry() ([]Item, error) {
	sess, names, err := readSessions(winscpKey)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, n := range names {
		if it, ok := winscpSession(n, sess[n]); ok {
			out = append(out, it)
		}
	}
	return out, nil
}

func loadPuttyRegistry() ([]Item, error) {
	sess, names, err := readSessions(puttyKey)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, n := range names {
		if it, ok := puttySession(winscpUnescape(n), sess[n]); ok {
			out = append(out, it)
		}
	}
	return out, nil
}
