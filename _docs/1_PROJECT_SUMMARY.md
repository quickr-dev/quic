# What
A CLI to instantly get an isolated, disposable branch of production database.

Usage example:
- `quic checkout pr-1234` # returns connection string
- `quic delete pr-1234` # deletes branch

# How
- pgbackrest restore of a production database backup
- `quic checkout` takes a zfs snapshot and zfs clone of the restore and starts a postgres instance on the clone
- zfs clones are storage-efficient

# Goals
- Save developers time, increase productivity
- Faster debugging of production issues
- Better QA on PR reviews with real production data
- Save company money by reducing wasted dev time and cloud costs
