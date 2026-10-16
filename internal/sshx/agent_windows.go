//go:build windows

package sshx

import (
	"errors"
	"net"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/davidmz/go-pageant"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentClient connects to the agent at path (a named pipe such as
// \\.\pipe\openssh-ssh-agent, or a Unix socket), else to Pageant or the
// Windows OpenSSH agent.
func agentClient(path string) (agent.Agent, func(), error) {
	timeout := 2 * time.Second
	if path != "" {
		var c net.Conn
		var err error
		if strings.HasPrefix(path, `\\.\pipe\`) || strings.HasPrefix(path, `//./pipe/`) {
			c, err = winio.DialPipe(path, &timeout)
		} else {
			c, err = net.DialTimeout("unix", path, timeout)
		}
		if err != nil {
			return nil, nil, err
		}
		return agent.NewClient(c), func() { c.Close() }, nil
	}
	if pageant.Available() {
		return pageant.New(), func() {}, nil
	}
	c, err := winio.DialPipe(`\\.\pipe\openssh-ssh-agent`, &timeout)
	if err != nil {
		return nil, nil, errors.New("neither Pageant nor the OpenSSH agent is running")
	}
	return agent.NewClient(c), func() { c.Close() }, nil
}

func agentSigners(path string) ([]ssh.Signer, error) {
	ag, _, err := agentClient(path)
	if err != nil {
		return nil, err
	}
	return ag.Signers()
}

func AgentAvailable() bool {
	_, closer, err := agentClient("")
	if err != nil {
		return false
	}
	closer()
	return true
}
