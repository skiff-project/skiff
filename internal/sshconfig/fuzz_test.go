package sshconfig

import "testing"

// The lines of ssh_config (the Match exec lines are not run here).
func FuzzLines(f *testing.F) {
	for _, s := range []string{`Host *.lan "a b"`, `IdentityFile "~/.ssh/id x"`, `ProxyCommand ssh -W %h:%p bastion`, `Port=2222`, `Match host a,b user !root`, `a "unterminated`} {
		f.Add(s, "*.lan,!x.lan", "x.lan")
	}
	f.Fuzz(func(t *testing.T, ln, pattern, host string) {
		splitLine(ln)
		splitArgs(ln)
		wildcard(pattern, host)
		matchList(host, pattern, true)
		ParseHop(ln)
		ParseForward("local", ln)
	})
}
