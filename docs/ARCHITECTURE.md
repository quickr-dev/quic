# Quic Architecture

## Principles

- **Development Team Productivity**: fast branch creation, no data corruption, fresh data, team collaboration, operational simplicity
- **Security**: encryption in transit, encryption at rest (deployment dependent), firewall rules, audit trail.
- **Storage Efficiency**: Leverage ZFS Copy-on-Write to minimize storage. See DATA_SYNC_METHODS.md for more details.

## Components

### CLI Client
- Communication with remote agent server
- User authentication and token management
- Branch management

### Agent Server
- Manage database branches
- Manage system resources and security
- Audit trail

## Design Decisions

### PostgreSQL Crash Recovery vs. pg_resetwal
- **Decision**: Allow PostgreSQL crash recovery on clone startup
- **Rationale**:
  - Maintains data consistency and ACID properties
  - Eliminates risk of subtle corruption from WAL reset
  - Predictable recovery time vs. unpredictable corruption
- **Trade-offs**: Slightly longer startup time vs. instant but risky

### systemd Service Management
- **Decision**: Individual systemd services per clone
- **Rationale**:
  - Process isolation and resource management
  - Standard Linux service lifecycle management
  - Built-in restart and monitoring capabilities
  - Clean integration with system logs
- **Trade-offs**: Service proliferation vs. process management complexity
