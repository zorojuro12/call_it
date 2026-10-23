#!/usr/bin/env bash
# Installs callit-secrets.service: sudo bash deploy/install-secrets.sh
# Rerun it whenever deploy/secrets.sh or the unit changes.
#
# The unit runs a root-owned copy of secrets.sh, not the file in this
# clone: a root service shouldn't execute a script the deploy user can
# edit.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
fail() { echo "install-secrets: $*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || fail "run with sudo"
[ -s /etc/callit/op-service-account-token ] \
  || fail "/etc/callit/op-service-account-token is missing; store the service account token first (deploy/README.md §4)"
command -v op >/dev/null || fail "the 1Password CLI (op) is not installed"
getent group docker >/dev/null || fail "no docker group; install Docker first"

# /usr/local/libexec doesn't exist on a stock Ubuntu install.
install -d -o root -g root -m 0755 /usr/local/libexec
install -o root -g root -m 0755 "$here/secrets.sh" /usr/local/libexec/callit-secrets
install -o root -g root -m 0644 "$here/systemd/callit-secrets.service" /etc/systemd/system/callit-secrets.service
systemctl daemon-reload
systemctl enable callit-secrets.service
echo "install-secrets: installed; start it with: sudo systemctl start callit-secrets"
