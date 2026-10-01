#!/usr/bin/env bash
# Bootstrap once using the administrator's existing SSH access, never from CI.
set -euo pipefail
cd "$(dirname "$0")/.."
test "$(id -u)" = 0
test -f /opt/river/.env
test -L /opt/river/current
test -f /opt/river/compose.production.yaml
test -f /opt/river/deployed-version
test -f /etc/nginx/sites-available/river.skyli.xyz
command -v python3 >/dev/null
python3 -c 'import sys; assert sys.version_info >= (3, 12), "Python 3.12+ is required"'
umask 077
install -d -m 700 /opt/river/cd /opt/river/backups
install -m 700 scripts/cd.py /opt/river/cd/cd.py
python3 - <<'PY'
from pathlib import Path
path = Path('/etc/nginx/sites-available/river.skyli.xyz')
text = path.read_text()
gate = '        if (-f /opt/river/maintenance) { return 503; }\n'
if gate not in text:
    location = '    location / {\n        proxy_pass http://127.0.0.1:18080;'
    assert location in text, 'Expected the existing RIVER upstream; inspect the virtual host before installing'
    backup = Path('/opt/river/cd/nginx-before-cd.conf')
    if not backup.exists():
        backup.write_text(text)
    path.write_text(text.replace(location, '    location / {\n' + gate + '        proxy_pass http://127.0.0.1:18080;', 1))
PY
nginx -t
systemctl reload nginx
cat > /etc/systemd/system/river-deploy.service <<'EOF'
[Unit]
Description=Publish a checked RIVER release when rooms are idle
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target

[Service]
Type=oneshot
User=root
UMask=0077
ExecStart=/usr/bin/python3 /opt/river/cd/cd.py deploy
TimeoutStartSec=300
EOF
cat > /etc/systemd/system/river-deploy.timer <<'EOF'
[Unit]
Description=Check the RIVER release queue every minute

[Timer]
OnBootSec=30s
OnUnitActiveSec=60s
AccuracySec=5s
Persistent=true

[Install]
WantedBy=timers.target
EOF
systemctl daemon-reload
systemctl enable --now river-deploy.timer
echo 'Installed RIVER CD receiver, maintenance gate and release queue timer'
