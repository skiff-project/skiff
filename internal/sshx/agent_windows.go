//go:build windows

package sshx

import (
	"errors"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/davidmz/go-pageant"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func agentClient() (agent.Agent, func(), error) {
	if pageant.Available() {
		return pageant.New(), func() {}, nil
	}
	timeout := 2 * time.Second
	c, err := winio.DialPipe(`\\.\pipe\openssh-ssh-agent`, &timeout)
	if err != nil {
		return nil, nil, errors.New("neither Pageant nor the OpenSSH agent is running")
	}
	return agent.NewClient(c), func() { c.Close() }, nil
}

func agentSigners() ([]ssh.Signer, error) {
	ag, _, err := agentClient()
	if err != nil {
		return nil, err
	}
	return ag.Signers()
}

func AgentAvailable() bool {
	_, closer, err := agentClient()
	if err != nil {
		return false
	}
	closer()
	return true
}
