#!/bin/bash

set -e

# Install ansible if not present
if ! command -v ansible-playbook &> /dev/null; then
    echo "Installing ansible..."
    apt-get update && apt-get install -y ansible
fi

# Parse command line arguments
CERT_EMAIL=""
CERT_DOMAIN=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --cert-email)
            CERT_EMAIL="$2"
            shift 2
            ;;
        --cert-domain)
            CERT_DOMAIN="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 --cert-email <cert_email> --cert-domain <cert_domain>"
            echo "Example: $0 --cert-email 'admin@example.com' --cert-domain 'db.example.com'"
            exit 0
            ;;
        *)
            echo "Unknown option $1"
            exit 1
            ;;
    esac
done

# Validate required parameters
if [ -z "$CERT_EMAIL" ] || [ -z "$CERT_DOMAIN" ]; then
    echo "ERROR: All parameters are required"
    echo "Usage: $0 --cert-email <cert_email> --cert-domain <cert_domain>"
    echo "Example: $0 --cert-email 'admin@example.com' --cert-domain 'db.example.com'"
    exit 1
fi

# Run ansible playbook
echo "Setting up Let's Encrypt certificates..."
curl -fsSL https://raw.githubusercontent.com/quickr-dev/quic/main/ansible/letsencrypt-setup.yml -o letsencrypt-setup.yml
ansible-playbook letsencrypt-setup.yml \
    -e "cert_email=$CERT_EMAIL" \
    -e "cert_domain=$CERT_DOMAIN"

# Clean up downloaded file if successful
if [ $? -eq 0 ]; then
    echo "Done"
    rm -f letsencrypt-setup.yml
else
    exit 1
fi