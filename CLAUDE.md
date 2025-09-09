# MANDATORY: Always do this first thing
- BEFORE doing ANYTHING else, run `find internal -type f` to view existing code.

# Project context
@README.md

- PostgreSQL database branching system using ZFS.
- Go-based CLI and gRPC agent server.

# Tests
- Don't clean up resources in tests. We prefer recreating/restoring the VM.
- Whenever possible, be smart about how you write tests so subsequent runs pass without restoring/recreating the VM.
- Use `bin/e2e <file-path-or-test-name>` to run quic CLI e2e tests.
- Check for existing helpers under @e2e/cli/ before creating new ones.
