_Disclaimer: quic is in early development. Use it at your own risk._

# Quic

Get ready-to-work, up-to-date, isolated branches of your Postgres database in seconds, not hours.

Great for development git workflows, PR/staging/demo environments, bug investigation, migration tests, and more.

# Getting Started

### Install quic

```sh
curl -fsSL https://raw.githubusercontent.com/quickr-dev/quic/main/scripts/install.sh | bash
```

Or download the binary directly from the [releases page](https://github.com/quickr-dev/quic/releases).

### Setup a host

Make sure you have ssh access to your host and run:

```sh
# 1. Host setup
quic host new <ip-address>
quic host setup

# 2. Template setup
quic template new <template-name>
quic template setup

# 3. Create user for yourself
quic user create "Your Name" # outputs an auth token
quic login --token <token>

# 3.1. Create users for team members & hand them their respective auth tokens
#      You may also want to create a specific user to use in CI
quic user create "Team Member"

# 4. Create branches
quic checkout <branch-name> # outputs a connection string

# 5. List branches
quic ls

# 6. Delete branches
quic delete <branch-name>
```

## License
[Business Source License 1.1](./LICENSE)
