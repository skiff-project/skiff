//go:build !windows

package sshx

import (
	"errors"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentClient connects to the agent at path, or to $SSH_AUTH_SOCK.
func agentClient(path string) (agent.Agent, func(), error) {
	sock := path
	if sock == "" {
		sock = os.Getenv("SSH_AUTH_SOCK")
	}
	if sock == "" {
		return nil, nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}
	return agent.NewClient(c), func() { c.Close() }, nil
}

func agentSigners(path string) ([]ssh.Signer, error) {
	ag, _, err := agentClient(path)
	if err != nil {
		return nil, err
	}
	// The connection stays open for the lifetime of the signers.
	return ag.Signers()
}

// AgentAvailable reports whether an agent can be reached.
func AgentAvailable() bool {
	_, closer, err := agentClient("")
	if err != nil {
		return false
	}
	closer()
	return true
}
