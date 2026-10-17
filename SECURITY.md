# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem. Report it
privately through GitHub: **Security → Report a vulnerability** on the
repository of Skiff. Say what an attacker can do, how to reproduce it
(Skiff version, system, protocol, server), and whether it is already
known elsewhere.

You get an answer within a week. Once the fix is released, the advisory
is published with your name if you wish.

## Supported versions

The security fixes go into the latest 1.x release; update to it.

## What Skiff does to protect you

The design is described in the documentation (in French):
[Sécurité de l'application](docs/11-securite.md#118-sécurité-de-lapplication)
and [Mode web distant](docs/14-mode-web.md#143-sécurité). In short:

- the window talks to the engine of Skiff on `127.0.0.1` only, with a
  random token, a `Host` check against DNS rebinding, a mandatory header
  against cross-site requests and a content security policy;
- host keys and TLS certificates are checked and pinned; saved passwords
  and keys are encrypted (with a master password: Argon2id and
  XChaCha20-Poly1305);
- names sent by servers never write outside of the destination folder;
  links (`sftp://`…) are always confirmed and cannot pin a host key;
- the web mode needs a password, limits the attempts and can be jailed
  in a folder (`--root`);
- the dependencies are checked for known vulnerabilities (`govulncheck`,
  `pnpm audit`) and the parsers of what servers send are fuzzed.

---

# Politique de sécurité (français)

Ne signalez pas une faille par un ticket public : utilisez **Security →
Report a vulnerability** sur le dépôt GitHub de Skiff (signalement privé),
avec ce qu'un attaquant peut faire et comment le reproduire. Une réponse
arrive sous une semaine ; l'avis est publié avec la correction, avec votre
nom si vous le souhaitez. Les correctifs vont dans la dernière version 1.x.
