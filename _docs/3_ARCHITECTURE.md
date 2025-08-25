# Server set up
Ref `ansible/deploy.yml`, `Makefile`
- Setup zfs, pg restore from CrunchyBridge backups, deploy quicd agent, setup systemd services
- TLS certs, strict firewall rules, encryption at rest, hashed auto tokens

# Auth
- Inidivial token by team member hardcoded in Ansible vault `ansible/group_vars/all/vault.yml`
- Manually sent to team members
- `quic login --token <auth_token>` saves token locally

# CLI
Ref `.github/workflows/release.yml`, `Makefile`
- Binaries generated via Github action in private repository and pushed to public repo `https://github.com/quickr-dev/quic-cli`
- Auto-updates based on `https://github.com/quickr-dev/quic-cli/blob/main/VERSION`

### `quic checkout pr-1234`
Ref `internal/agent/checkout.go`
- zfs snapshot & clone template db
- creates systemd for clone
- finds and exposes an available port
- returns connection string

### `quic delete pr-1234`
Ref `internal/agent/delete.go`
- Undoes `quic checkout`
