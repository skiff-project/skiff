//go:build !windows

package sshx

import (
	"errors"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func agentClient() (agent.Agent, func(), error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}
	return agent.NewClient(c), func() { c.Close() }, nil
}

func agentSigners() ([]ssh.Signer, error) {
	ag, _, err := agentClient()
	if err != nil {
		return nil, err
	}
	// The connection stays open for the lifetime of the signers.
	return ag.Signers()
}

// AgentAvailable reports whether an agent can be reached.
func AgentAvailable() bool {
	_, closer, err := agentClient()
	if err != nil {
		return false
	}
	closer()
	return true
}
