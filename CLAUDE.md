# About the project
- @README.md: Project introduction, problems solved, use cases
- @docs/ARCHITECTURE.md: system architecture and design decisions
- `./Makefile`: tasks to build, deploy, test

## Key Technical Context
- Database branching system using ZFS snapshots and PostgreSQL coordination
- Go-based CLI and gRPC agent server
- systemd service management for PostgreSQL clones
- Ansible for setting up the environment
- Token-based authentication
