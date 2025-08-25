# Attempts

### Local VM
Tried to use local multipass VM but the network bandwidth seems limited.
`bench/pgbackrest-macos-vm.sh`

### Direct on MacOS
MacOS doesn't seem to have a tool like zfs.
Also, database will soon grow big enough that it won't fit my 1TB storage.

# pg dump of local db
Took 225m13.068s
`bench/pgbackrest-macos-vm.sh`

# Statistics

### Db size
~100 GB / month
2025-07-24 - 540GB
2025-08-22 - 650GB
https://crunchybridge.com/clusters/p2jxovzcs5e3ff2cwmjngnt7iq/metrics/disk-usage?period=30d

### Nightly backups (12h+)
Timing out
2025-08-22: https://github.com/botsandus/DexoryView/actions/runs/17143520328

### Time to "Load Production DB" label
14:26:49 15:07:17 = ~40min
```sh
[2025-08-22 14:26:46 +0000] SELECT COUNT(*) AS count FROM deployments
[2025-08-22 14:26:48 +0000] Dropping existing database ra_pr_3068_dexory_db if exists
[2025-08-22 14:26:48 +0000] DROP DATABASE IF EXISTS ra_pr_3068_dexory_db WITH (FORCE);
[2025-08-22 14:26:49 +0000] Cloning nightly_production_clone into ra_pr_3068_dexory_db using file copy strategy
[2025-08-22 14:26:49 +0000]
        	CREATE DATABASE ra_pr_3068_dexory_db OWNER application TEMPLATE nightly_production_clone STRATEGY FILE_COPY

[2025-08-22 15:07:17 +0000] TRUNCATE ONLY good_job_executions, good_job_batches, good_jobs
[2025-08-22 15:07:17 +0000] PG::ConnectionBad: PQconsumeInput() FATAL:  terminating connection due to administrator command
SSL connection has been closed unexpectedly
[2025-08-22 15:07:17 +0000] retrying
[2025-08-22 15:07:47 +0000] TRUNCATE ONLY good_job_executions, good_job_batches, good_jobs
[2025-08-22 15:07:48 +0000] ANALYZE
[2025-08-22 15:14:36 +0000] Deploying app, this may take some time
```

### Active review apps (21)
```sh
[2025-08-22 15:19:33 +0000] The PRs that should have review apps are: 3068, 3061, 3060, 3057, 3055, 3053, 3028, 3015, 2999, 2959, 2951, 2946, 2937, 2855, 2698, 2682, 2658, 2425, 2025, 2023, 1832
```

### Existing DBs in staging (15)
```sh
ra_pr_3068_dexory_db	500 GB
ra_pr_3060_dexory_db	497 GB
ra_pr_2937_dexory_db	497 GB
ra_pr_3053_dexory_db	495 GB
ra_pr_3015_dexory_db	483 GB
ra_pr_2999_dexory_db	481 GB
ra_pr_3057_dexory_db	21 MB
ra_pr_3055_dexory_db	21 MB
ra_pr_3028_dexory_db	21 MB
ra_pr_2425_dexory_db	21 MB
ra_pr_2951_dexory_db	20 MB
ra_pr_2682_dexory_db	20 MB
ra_pr_2855_dexory_db	20 MB
ra_pr_2959_dexory_db	19 MB
ra_pr_2946_dexory_db	19 MB
```

### bloated tables?
Production: `dexoryview_production | 646 GB`
After pg_dump: `dexoryview_production | 501 GB`
Using:
```sql
SELECT datname AS database_name,pg_size_pretty(pg_database_size(datname))AS size FROM pg_database ORDER BY pg_database_size(datname)DESC;
```

### zfs compression (~28%)
Production restore: `dexoryview_production | 646 GB`
```sh
ubuntu@quic:~$ zfs list
NAME                                 USED  AVAIL  REFER  MOUNTPOINT
tank                                 466G  1.13T    96K  /tank
tank/dbs                             466G  1.13T    96K  /tank/dbs
tank/dbs/dexory                      466G  1.13T   104K  /tank/dbs/dexory
tank/dbs/dexory/dexoryview           466G  1.13T   104K  /tank/dbs/dexory/dexoryview
tank/dbs/dexory/dexoryview/restore   466G  1.13T   315G  /tank/dbs/dexory/dexoryview/restore
```
