# Comparison between physical replication, logical replication, and restores

Storage efficiency depends on how you keep your data fresh because ZFS Copy-on-Write snapshots amplify the impact of different update methods.

## TLDR

**Best choice:** Physical replication for ZFS snapshots
- Storage grows with actual changes only (~1GB/day for 1GB changes)
- Preserves incremental nature of database changes
- Maximizes block sharing across snapshots

**If you must use pg_restore:**
- Run restores less frequently (weekly/monthly instead of daily)
- Clean up old snapshots before restoring to control storage growth
- Coordinate snapshot cleanup with your team to avoid data loss

## Detailed Comparison

### Physical replication ✅ Recommended
- **Method:** PostgreSQL applies WAL (Write-Ahead Log) records incrementally
- **Storage impact:** Only changed blocks are written (~1:1 with actual changes)
- **Disruption:** None - continuous real-time updates
- **Best for:** Production environments with ZFS snapshots

### Logical replication ⚠️ Use with caution
- **Method:** Changes applied as SQL operations (INSERT/UPDATE/DELETE)
- **Storage impact:** 3-5x write amplification due to page rewrites
- **Disruption:** None - continuous updates via SQL
- **Best for:** When you need SQL-level transformations or filtering

### Database restore ❌ Avoid for frequent updates
- **Method:** Complete drop and recreate of entire database
- **Storage impact:** ~100x write amplification (full database rewrite)
- **Disruption:** Database unavailable during restore
- **Best for:** Initial setup or infrequent full refreshes

## Storage profiles scenario
Scenario:
- 100GB database with 1GB of actual data changes daily
- One new ZFS snapshot created each day
- No snapshot cleanup (all snapshots retained)
- Numbers show cumulative storage used by each snapshot

### Physical replication (1GB daily growth)
- Day 1: Database: 100GB → Create snapshot1 (stores: minimal overhead)
- Day 2: Database: 101GB → snapshot1 now stores: 1GB of changed blocks → Create snapshot2 (stores: minimal)
- Day 3: Database: 102GB → snapshot1: 2GB, snapshot2: 1GB → Create snapshot3 (stores: minimal)
- **Total snapshot storage after 3 days: ~3GB** (only actual changes)

### Logical Replication (3-5GB daily growth)
- Day 1: Database: 100GB → Create snapshot1 (stores: minimal overhead)
- Day 2: Database: 101GB → snapshot1 now stores: ~3-5GB → Create snapshot2 (stores: minimal)
- Day 3: Database: 102GB → snapshot1: ~6-10GB, snapshot2: ~3-5GB → Create snapshot3 (stores: minimal)
- **Total snapshot storage after 3 days: ~9-15GB** (3-5x amplification due to page rewrites)
- Note: Write amplification occurs because PostgreSQL rewrites entire 8KB pages even for small changes, plus VACUUM operations

### Database restore (100GB+ daily growth)
- Day 1: Database: 100GB → Create snapshot1 (stores: minimal overhead)
- Day 2: Drop & restore 101GB database → snapshot1 now stores: ~100GB of old blocks → Create snapshot2 (stores: minimal)
- Day 3: Drop & restore 102GB database → snapshot1: ~100GB, snapshot2: ~101GB → Create snapshot3 (stores: minimal)
- **Total snapshot storage after 3 days: ~201GB** (nearly full copies)
- Note: Every restore writes ALL blocks as new, even if 99% of data is unchanged

## Technical Explanation

### Why physical replication is so efficient
- WAL records contain only the bytes that changed within a page
- PostgreSQL surgically updates specific 8KB pages
- Untouched tables keep their blocks 100% shared with snapshots
- **Result:** ZFS snapshot growth = actual data changes

### Why logical replication causes write amplification
- PostgreSQL must rewrite entire 8KB pages for any change
- A single row update rewrites the whole page (affecting neighboring rows)
- VACUUM processes rewrite pages for space reclamation
- Index maintenance triggers additional page writes
- **Result:** 3-5x more blocks modified than actual data changes

### Why restore is catastrophically inefficient
- `pg_restore` doesn't check if data already exists - it always writes "new"
- Drops all tables/indexes first, then recreates everything from scratch
- Even if 99% of your data is identical, 100% gets rewritten
- **Result:** Every single block in the database becomes unique to that snapshot
