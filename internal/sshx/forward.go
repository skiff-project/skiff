package sshx

import (
	"errors"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// forwardAgent is the agent a server sees with agent forwarding: the keys
// of Skiff's keychain, then those of the system agent. The server may list
// keys and ask for signatures, never add or remove keys.
type forwardAgent struct {
	keys []ssh.Signer
	sys  agent.Agent // may be nil
}

var errReadOnlyAgent = errors.New("the Skiff agent cannot be modified from the server")

func (a *forwardAgent) List() ([]*agent.Key, error) {
	var out []*agent.Key
	for _, s := range a.keys {
		pub := s.PublicKey()
		out = append(out, &agent.Key{Format: pub.Type(), Blob: pub.Marshal(), Comment: "Skiff keychain"})
	}
	if a.sys != nil {
		if more, err := a.sys.List(); err == nil {
			out = append(out, more...)
		}
	}
	return out, nil
}

func (a *forwardAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return a.SignWithFlags(key, data, 0)
}

func (a *forwardAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	for _, s := range a.keys {
		if !sameKey(s.PublicKey(), key) {
			continue
		}
		if as, ok := s.(ssh.AlgorithmSigner); ok {
			switch {
			case flags&agent.SignatureFlagRsaSha256 != 0:
				return as.SignWithAlgorithm(nil, data, ssh.KeyAlgoRSASHA256)
			case flags&agent.SignatureFlagRsaSha512 != 0:
				return as.SignWithAlgorithm(nil, data, ssh.KeyAlgoRSASHA512)
			}
		}
		return s.Sign(nil, data)
	}
	if a.sys == nil {
		return nil, errors.New("key not found")
	}
	if ea, ok := a.sys.(agent.ExtendedAgent); ok {
		return ea.SignWithFlags(key, data, flags)
	}
	return a.sys.Sign(key, data)
}

func (a *forwardAgent) Signers() ([]ssh.Signer, error) {
	out := append([]ssh.Signer(nil), a.keys...)
	if a.sys != nil {
		if more, err := a.sys.Signers(); err == nil {
			out = append(out, more...)
		}
	}
	return out, nil
}

func (a *forwardAgent) Add(agent.AddedKey) error   { return errReadOnlyAgent }
func (a *forwardAgent) Remove(ssh.PublicKey) error { return errReadOnlyAgent }
func (a *forwardAgent) RemoveAll() error           { return errReadOnlyAgent }
func (a *forwardAgent) Lock([]byte) error          { return errReadOnlyAgent }
func (a *forwardAgent) Unlock([]byte) error        { return errReadOnlyAgent }
func (a *forwardAgent) Extension(string, []byte) ([]byte, error) {
	return nil, agent.ErrExtensionUnsupported
}

// RequestAgentForwarding asks the server to give sess access to the
// forwarded agent; it does nothing when forwarding is off.
func (c *Conn) RequestAgentForwarding(sess *ssh.Session) error {
	if !c.forward {
		return nil
	}
	return agent.RequestAgentForwarding(sess)
}

// ForwardsAgent reports whether the agent is forwarded on this connection.
func (c *Conn) ForwardsAgent() bool { return c.forward }
