_Disclaimer: quic is in early development and not recommended for production yet. Use it at your own risk._

# Quic

Create ready-to-work, isolated branches of your Postgres database in seconds, not hours.

```sh
$ quic checkout <branch-name>
> postgresql://admin:xyz@server:5433/production?sslmode=verify-full&sslrootcert=system

$ quic delete <branch-name>
```

## Why Quic?
- **Productivity**: ready to work in seconds vs hours with pg_dump/restore
- **Storage efficient**: leverages ZFS copy-on-write
- **Better dev and QA**: quickly work with fresh, real data
- **Zero hassle**: quic manages resource creation and cleanup
- **Secure & isolated**: dedicated ports, TLS, firewall rules


## Get started

#### Host setup

Scripts are currently designed to run in your host.

```bash
ssh into.your.server

# list devices
lsblk

# base system setup (includes self-signed TLS certificates)
curl -fsSL https://raw.githubusercontent.com/quickr-dev/quic/main/scripts/base-setup.sh | \
sudo bash -s -- \
  --devices 'nvme0n1,nvme1n1' \
  --pg-version '16'

# add trusted TLS certificates (recommended but optional)
curl -fsSL https://raw.githubusercontent.com/quickr-dev/quic/main/scripts/letsencrypt-setup.sh | \
sudo bash -s -- \
  --cert-email 'admin@domain.com' \
  --cert-domain 'domain.com'
```

## Use Cases
Perfect for development git workflows, PR/staging/demo environments, bug investigation.

## Documentation
- [Architecture](docs/ARCHITECTURE.md)

## License
[Business Source License 1.1](https://github.com/hashicorp/terraform/blob/main/LICENSE)
