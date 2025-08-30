#!/bin/bash

set -e

# Install deps
if ! command -v ansible-playbook &> /dev/null; then
    echo "Installing ansible..."
    apt-get update && apt-get install -y ansible
fi

# Parse command line arguments
DEVICES=""
CERT_EMAIL=""
CERT_DOMAIN=""
PG_VERSION=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --devices)
            DEVICES="$2"
            shift 2
            ;;
        --cert-email)
            CERT_EMAIL="$2"
            shift 2
            ;;
        --cert-domain)
            CERT_DOMAIN="$2"
            shift 2
            ;;
        --pg-version)
            PG_VERSION="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 --devices <devices> --cert-email <cert_email> --cert-domain <cert_domain> --pg-version <pg_version>"
            echo "Example: $0 --devices 'nvme0n1,nvme1n1' --cert-email 'admin@example.com' --cert-domain 'db.example.com' --pg-version '16'"
            exit 0
            ;;
        *)
            echo "Unknown option $1"
            exit 1
            ;;
    esac
done

# Validate required parameters
if [ -z "$DEVICES" ] || [ -z "$CERT_EMAIL" ] || [ -z "$CERT_DOMAIN" ] || [ -z "$PG_VERSION" ]; then
    echo "ERROR: All parameters are required"
    echo "Usage: $0 --devices <devices> --cert-email <cert_email> --cert-domain <cert_domain> --pg-version <pg_version>"
    echo "Example: $0 --devices 'nvme0n1,nvme1n1' --cert-email 'admin@example.com' --cert-domain 'db.example.com' --pg-version '16'"
    exit 1
fi

# Convert comma-separated devices to array
IFS=',' read -ra DEVICE_ARRAY <<< "$DEVICES"

for device in "${DEVICE_ARRAY[@]}"; do
    device_path="/dev/$device"

    # Check if device exists
    if [ ! -b "$device_path" ]; then
        echo "Error: Device $device_path does not exist"
        exit 1
    fi

    # Check for existing partitions
    if lsblk -n -o NAME "$device" 2>/dev/null | grep -q "├\|└"; then
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

    echo "✓ Device $device_path"
done

# Run ansible playbook
echo "Running ansible playbook..."
curl -fsSL https://raw.githubusercontent.com/quickr-dev/quic/main/ansible/base-setup.yml -o base-setup.yml
ansible-playbook base-setup.yml \
    -e "zfs_devices=[$(echo "$DEVICES" | sed 's/,/,/g' | sed 's/[^,]*/"&"/g')]" \
    -e "pg_version=$PG_VERSION"

# Clean up downloaded file if successful
if [ $? -eq 0 ]; then
    echo "Done"
    rm -f base-setup.yml
else
    exit 1
fi
