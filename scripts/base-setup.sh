#!/bin/bash

set -e

# Install deps
if ! command -v ansible-playbook &> /dev/null; then
    echo "Installing ansible..."
    apt-get update && apt-get install -y ansible
fi

# Parse command line arguments
DEVICES=""
PG_VERSION=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --devices)
            DEVICES="$2"
            shift 2
            ;;
        --pg-version)
            PG_VERSION="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 --devices <devices> --pg-version <pg_version>"
            echo "Example: $0 --devices 'nvme0n1,nvme1n1' --pg-version '16'"
            exit 0
            ;;
        *)
            echo "Unknown option $1"
            exit 1
            ;;
    esac
done

# Validate required parameters
if [ -z "$DEVICES" ] || [ -z "$PG_VERSION" ]; then
    echo "ERROR: All parameters are required"
    echo "Usage: $0 --devices <devices> --pg-version <pg_version>"
    echo "Example: $0 --devices 'nvme0n1,nvme1n1' --pg-version '16'"
    exit 1
fi

# Convert comma-separated devices to array
IFS=',' read -ra DEVICE_ARRAY <<< "$DEVICES"

for device_path in "${DEVICE_ARRAY[@]}"; do
    # Extract device name for lsblk (remove /dev/ prefix)
    device_name=$(basename "$device_path")

    # Check if device/file exists
    if [ -b "$device_path" ]; then
        # It's a block device - do block device checks
        echo "Using block device: $device_path"

        # Check for existing partitions
        if lsblk -n -o NAME "$device_name" 2>/dev/null | grep -q "├\|└"; then
            echo "Safety failure: Device $device_path has existing partitions"
            echo "Please wipe the device: wipefs -a $device_path && dd if=/dev/zero of=$device_path bs=1M count=100"
            exit 1
        fi

        # Check for filesystem signatures
        if blkid "$device_path" 2>/dev/null; then
            echo "Safety failure: Device $device_path has existing filesystem"
            echo "Please wipe the device: wipefs -a $device_path && dd if=/dev/zero of=$device_path bs=1M count=100"
            exit 1
        fi

    elif [ -f "$device_path" ]; then
        # It's a regular file - just check it exists and has reasonable size
        echo "Using file: $device_path"

    else
        echo "Error: $device_path is neither a block device nor a regular file"
        exit 1
    fi

    echo "✓ $device_path"
done

# Run ansible playbook
echo "Running ansible playbook..."

# e2e tests use local base-setup.yml
if [ -f "/var/lib/quic/quic/ansible/base-setup.yml" ]; then
    echo "Using local base-setup.yml"
    PLAYBOOK_PATH="/var/lib/quic/quic/ansible/base-setup.yml"
else
    echo "Downloading base-setup.yml from GitHub"
    curl -fsSL https://raw.githubusercontent.com/quickr-dev/quic/refs/heads/main/internal/cli/assets/base-setup.yml -o base-setup.yml
    PLAYBOOK_PATH="base-setup.yml"
fi

ansible-playbook "$PLAYBOOK_PATH" \
    -e "zfs_devices=$DEVICES" \
    -e "pg_version=$PG_VERSION"

if [ $? -eq 0 ]; then
    echo "Done"
    rm -f base-setup.yml
else
    exit 1
fi
