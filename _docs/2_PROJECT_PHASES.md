# Phase 1: evaluate
- Use the solution for PRs and UK team members local environments
- Keep machine same as CrunchyBridge: 8 cores / 32 gb
  - 2 TB NVMe
    - In RAID 0 (should be enough for this phase, zfs snapshots are storage-efficient)
  - ~$100-$200 / month
  - Monitor resource usage and increase specs if needed

# Phase 2: integrate & expand
- Use the solution in staging and demo environments (saves ~$2k/month if we can replace CrunchyBridge)
- Optionally, based on dev adoption in Phase 1, we could evaluate getting servers in Canada & Brazil for the remaining team members
  - ~$100-$150 per server

# Risks & mitigations
### PR review disruption
To mitigate this, in phase 1, we can add the branch instance as the first server in `postgresql://` connection string, leaving CrunchyBridge as fallback.

### Ongoing dev, maintenance, incident support, spreading knowledge, etc.
Happy to own these responsibilities.
That could alleviate some of Alex and Elliot's responsibilities to focus on other things.

### Affects production performance?
No, the template DB is restored from CrunchyBridge's backups.

### Data breach mitigation, compliance
Proper security measures are already in place:
- All connections use TLS
- Encryption at rest
- Firewall exposes only the clones, never the hot standby template
- TODO: de-identify data in the clones
- If necessary, there are alternatives besides having a hot standby with production data on these servers.
