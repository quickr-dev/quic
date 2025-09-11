# MANDATORY: Always do this first thing
- BEFORE doing ANYTHING else, run `find internal -type f` to view existing code.

# Project context
- @README.md
- PostgreSQL database branching system using ZFS.
- Go-based CLI and gRPC agent server.

# Tests
- We have CLI-focused e2e tests under @e2e/cli/ which check VM state
  . To setup a VM, runQuicHostSetupWithAck runs `quic setup` which runs ansible `internal/cli/assets/base-setup.yml`
  . the base-setup.yml downloads quicd from Github releases to install it in the VM
  . we then build and replace it in the VM to test our local code.
- Don't clean up resources in tests. We prefer recreating/restoring the VM.
- Use `DEBUG=1 bin/e2e <file-path-or-test-name>` to run quic CLI e2e tests.
