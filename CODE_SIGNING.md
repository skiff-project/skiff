# Code signing policy

**Status:** application to [SignPath Foundation](https://signpath.org) in progress. Until it is accepted, the Windows programs and installers of Skiff are **not signed**, and Windows warns before starting them (see the [installation chapter](docs/en/01-installation.md)).

Once accepted, this applies: *Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).*

## What is signed

- The Windows programs (`skiff-windows-amd64.exe`, `skiff-windows-arm64.exe`) and installers (`skiff-setup-<version>-windows-<arch>.exe`) of the releases.
- They are built by GitHub Actions ([`.github/workflows/build.yml`](.github/workflows/build.yml)) from this repository, from a `v*` tag, and signed by SignPath during that build: the programs first, then the installers made from the signed programs ([`.signpath/`](.signpath/artifact-configurations)). Nothing is built or signed by hand.
- Each signing request is approved by hand in SignPath before the certificate is used.
- The Microsoft Store package is signed by Microsoft; the macOS disk image is signed ad hoc only (not notarized).

## Team

| Role | Members |
|---|---|
| Committers and reviewers | [Laterna](https://github.com/laterna-media) |
| Approvers (signing requests) | [Laterna](https://github.com/laterna-media) |

## Privacy

Skiff sends no data to Laterna or to anyone else, and connects only to the servers its user sets up: see the [privacy policy](PRIVACY.md).

---

🇫🇷 **En français :** les programmes et installateurs Windows des versions publiées seront signés par SignPath Foundation (signature gratuite pour le logiciel libre) une fois la demande acceptée ; d'ici là, ils ne sont pas signés. Ils sont compilés et signés uniquement par GitHub Actions depuis ce dépôt, chaque demande de signature étant approuvée à la main par Laterna. Voir aussi la [politique de confidentialité](PRIVACY.md).
