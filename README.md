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

## Use Cases
Perfect for development git workflows, PR/staging/demo environments, bug investigation.

## Documentation
- [Architecture](docs/ARCHITECTURE.md)

## License
[Business Source License 1.1](https://github.com/hashicorp/terraform/blob/main/LICENSE)
