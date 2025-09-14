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

### quic.json
A `quic.json` file will be created to hold configuration used for setting up infrastructure and managing branches.

### Setup a host
Make sure you have ssh access to your host and run:

```sh
quic host new <ip-address>
quic host setup
```

### Setup a template database
For now, it just works for CrunchyBridge backups. Feel free to create an issue detailing your use case.

```sh
quic template new <template-name>
quic template setup
```

### Create a user for yourself
```sh
quic user create "Your Name" # outputs an auth token
quic login --token <token>
```

You may also want to create users for each team member and CI.

For now, you need to manually hand auth tokens to the respective team members.

```sh
quic user create "Team Member"
quic user create "CI"
```

### Create branches
```sh
quic checkout <branch-name> # outputs a connection string
```

### List branches
```sh
quic ls
```

### Delete branches
```sh
quic delete <branch-name>
```

## License
[Business Source License 1.1](./LICENSE)
