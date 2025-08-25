# Connection Settings - Keep flexible for clones but minimal for template
listen_addresses = '*'
max_connections = 100
tcp_keepalives_count = 4
tcp_keepalives_idle = 2
tcp_keepalives_interval = 2

# Security Settings - Keep for clones that might need them
ssl = on
ssl_ca_file = 'client_ca.pem'
ssl_min_protocol_version = TLSv1.2
ssl_ciphers = 'TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384'
password_encryption = scram-sha-256

# Memory Settings - Minimal for template, clones will override
shared_buffers = 512MB  # Enough for WAL replay
work_mem = 32MB
maintenance_work_mem = 256MB
effective_cache_size = 4GB
effective_io_concurrency = 68
random_page_cost = 1.1

# WAL Settings - CRITICAL for active standby
wal_level = replica  # Required for streaming replication
archive_mode = off   # Standby doesn't archive (already off in auto.conf)
max_wal_size = 10GB  # Keep same as primary for consistency
min_wal_size = 80MB
checkpoint_completion_target = 0.9
wal_keep_size = 1GB  # Keep some WAL in case of temporary network issues

# Replication Settings - CRITICAL for active syncing
hot_standby = on  # Allow read-only queries (though template won't use)
hot_standby_feedback = on  # IMPORTANT: Prevent query conflicts
max_standby_streaming_delay = 30s
max_standby_archive_delay = 30s
wal_receiver_status_interval = 10s
wal_receiver_timeout = 60s
wal_retrieve_retry_interval = 5s

# Remove production-specific sync settings
# synchronous_standby_names removed - standby doesn't need this
# synchronous_commit removed - standby doesn't need this
# max_slot_wal_keep_size removed - standby doesn't need this

# Worker Processes - Reasonable defaults
max_worker_processes = 8
max_parallel_workers = 4
max_parallel_workers_per_gather = 2
max_wal_senders = 0  # In case clones need to cascade

# File Limits
max_files_per_process = 10000
temp_file_limit = '256.0GB'

# Extensions - Keep useful dev extensions
shared_preload_libraries = 'pg_stat_statements,pgaudit'

# Logging - Good defaults for development
logging_collector = on
log_destination = 'syslog'
log_file_mode = 0640
log_filename = 'postgresql-%a.log'
log_line_prefix = '[%p][%b][%v][%x] %q[user=%u,db=%d,app=%a] '
log_rotation_age = '1d'
log_truncate_on_rotation = on
log_checkpoints = on
log_lock_waits = on
log_statement = ddl
log_temp_files = '10MB'
log_min_messages = 'notice'
log_min_duration_statement = '5s'
log_connections = off
log_disconnections = off

# Performance Tracking
track_io_timing = on
