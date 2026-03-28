#!/bin/bash
set -euo pipefail

# SageClaw Relay Server — Ubuntu Setup Script
# Run as root on a fresh Ubuntu 22.04/24.04 VPS.
#
# Prerequisites:
#   - Domain sageclaw.io with DNS access
#   - DNS records already configured (see Step 1 in guide)
#
# Usage:
#   chmod +x setup.sh
#   sudo ./setup.sh

echo "=== SageClaw Relay Setup ==="

# 1. System updates
echo "[1/6] Updating system..."
apt-get update -qq && apt-get upgrade -y -qq

# 2. Install Docker
echo "[2/6] Installing Docker..."
if ! command -v docker &>/dev/null; then
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
    echo "Docker installed."
else
    echo "Docker already installed."
fi

# 3. Install Docker Compose plugin
echo "[3/6] Checking Docker Compose..."
if ! docker compose version &>/dev/null; then
    apt-get install -y -qq docker-compose-plugin
fi
docker compose version

# 4. Install nginx
echo "[4/6] Installing nginx..."
apt-get install -y -qq nginx
systemctl enable nginx

# 5. Install certbot with DNS plugin
echo "[5/6] Installing certbot..."
apt-get install -y -qq certbot python3-certbot-nginx
# For DNS-01 wildcard certs, you also need the DNS plugin for your provider.
# Example for Cloudflare:
#   apt-get install -y python3-certbot-dns-cloudflare
# Example for DigitalOcean:
#   apt-get install -y python3-certbot-dns-digitalocean

# 6. Firewall
echo "[6/6] Configuring firewall..."
ufw allow 22/tcp   # SSH
ufw allow 80/tcp   # HTTP (certbot + redirect)
ufw allow 443/tcp  # HTTPS
ufw --force enable

echo ""
echo "=== System setup complete ==="
echo ""
echo "Next steps:"
echo "  1. Configure DNS (see guide Step 1)"
echo "  2. Obtain wildcard TLS cert (see guide Step 3)"
echo "  3. Configure nginx (see guide Step 4)"
echo "  4. Deploy relay with Docker Compose (see guide Step 5)"
echo ""
