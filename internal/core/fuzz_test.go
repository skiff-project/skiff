package core

import "testing"

// The outputs of du, df, ss, systemctl, docker and /proc come from the
// server (or a container).
func FuzzServerOutputs(f *testing.F) {
	f.Add("4\t./a\n12\t.\nSKIFF:DIRS\n./a\nSKIFF:DF\n/dev/sda1 100 50 50 50% /\n")
	f.Add("SKIFF:SS\nLISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=812,fd=6))\n")
	f.Add("SKIFF:PROC\n  0: 0100007F:0BB8 00000000:0000 0A\n")
	f.Fuzz(func(t *testing.T, out string) {
		parseDu(out)
		parsePorts(out)
		parseServices(out)
		parseDockerPS(out)
		parseMonitor(out, map[int]string{0: "root"})
		parseRemoteShells(out)
		parsePasswd(out)
	})
}
