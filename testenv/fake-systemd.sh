#!/bin/sh
# Stand-ins for systemctl and journalctl on the SSH test server (Alpine has
# no systemd), for the tests and the screenshots of the services window.
#   docker exec -i -u 0 testenv-ssh-1 sh -s install < testenv/fake-systemd.sh
#   docker exec -i -u 0 testenv-ssh-1 sh -s remove  < testenv/fake-systemd.sh
# The state of the services is kept in /tmp/fake-systemd; protected.service
# refuses the actions of a user that is not root, like polkit does.
set -e

if [ "$1" = remove ]; then
  rm -f /usr/local/bin/systemctl /usr/local/bin/journalctl
  rm -rf /tmp/fake-systemd
  exit 0
fi

rm -rf /tmp/fake-systemd
mkdir -p /tmp/fake-systemd
chmod 1777 /tmp/fake-systemd
# name|state|sub|enabled|description
cat > /tmp/fake-systemd/units <<'EOF'
nginx.service|active|running|enabled|A high performance web server and a reverse proxy server
php8.2-fpm.service|failed|failed|enabled|The PHP 8.2 FastCGI Process Manager
mariadb.service|active|running|enabled|MariaDB 10.11 database server
ssh.service|active|running|enabled|OpenBSD Secure Shell server
cron.service|active|running|enabled|Regular background program processing daemon
redis-server.service|inactive|dead|disabled|Advanced key-value store
certbot.service|inactive|dead|static|Certbot
protected.service|active|running|enabled|A service only root may drive
EOF
chmod 666 /tmp/fake-systemd/units

cat > /usr/local/bin/systemctl <<'EOS'
#!/bin/sh
U=/tmp/fake-systemd/units
field() { grep "^$1|" "$U" | cut -d'|' -f"$2"; }
set_state() { awk -F'|' -v OFS='|' -v n="$1" -v s="$2" -v b="$3" '$1==n{$2=s;$3=b}1' "$U" > "$U.$$" && cat "$U.$$" > "$U" && rm -f "$U.$$"; }
set_enabled() { awk -F'|' -v OFS='|' -v n="$1" -v e="$2" '$1==n{$4=e}1' "$U" > "$U.$$" && cat "$U.$$" > "$U" && rm -f "$U.$$"; }
cmd=$1; shift
name=
for a in "$@"; do case "$a" in -*) ;; *) name=$a ;; esac; done
case "$cmd" in
list-units)
  while IFS='|' read -r n s sub en d; do printf '%s loaded %s %s %s\n' "$n" "$s" "$sub" "$d"; done < "$U" ;;
list-unit-files)
  while IFS='|' read -r n s sub en d; do printf '%s %s enabled\n' "$n" "$en"; done < "$U" ;;
show)
  s=$(field "$name" 2); sub=$(field "$name" 3)
  [ -z "$s" ] && { echo "Id=$name"; echo "LoadState=not-found"; exit 0; }
  echo "Id=$name"; echo "Description=$(field "$name" 5)"; echo "LoadState=loaded"
  echo "ActiveState=$s"; echo "SubState=$sub"; echo "UnitFileState=$(field "$name" 4)"
  if [ "$s" = active ]; then echo "MainPID=$((1000 + ${#name}))"; echo "MemoryCurrent=48234496"; else echo "MainPID=0"; echo "MemoryCurrent=[not set]"; fi
  echo "ActiveEnterTimestamp=Fri 2026-10-16 09:12:04 CEST"
  echo "InactiveEnterTimestamp=Fri 2026-10-16 08:58:41 CEST"
  echo "FragmentPath=/lib/systemd/system/$name"
  if [ "$s" = failed ]; then echo "Result=exit-code"; echo "ExecMainStatus=78"; echo "NRestarts=5"; else echo "Result=success"; echo "ExecMainStatus=0"; echo "NRestarts=0"; fi ;;
start|stop|restart|reload|enable|disable)
  if [ "$name" = protected.service ] && [ "$(id -u)" != 0 ]; then
    echo "Failed to $cmd $name: Interactive authentication required." >&2; exit 1
  fi
  [ -z "$(field "$name" 2)" ] && { echo "Failed to $cmd $name: Unit $name not found." >&2; exit 5; }
  case "$cmd" in
  start|restart|reload) set_state "$name" active running ;;
  stop) set_state "$name" inactive dead ;;
  enable) set_enabled "$name" enabled ;;
  disable) set_enabled "$name" disabled ;;
  esac ;;
*) echo "Unknown command verb $cmd." >&2; exit 1 ;;
esac
EOS

cat > /usr/local/bin/journalctl <<'EOS'
#!/bin/sh
unit=; n=10; follow=
while [ $# -gt 0 ]; do
  case "$1" in -u) unit=$2; shift ;; -n) n=$2; shift ;; -f) follow=1 ;; esac
  shift
done
name=${unit%.service}
i=1
while [ "$i" -le "$n" ] && [ "$i" -le 12 ]; do
  case $((i % 4)) in
  0) echo "2026-10-16T09:12:0$((i % 10))+0200 vps $name[$((1000 + i))]: GET /index.php 200 in 12 ms" ;;
  1) echo "2026-10-16T09:12:0$((i % 10))+0200 vps $name[$((1000 + i))]: worker $i ready" ;;
  2) echo "2026-10-16T09:12:0$((i % 10))+0200 vps $name[$((1000 + i))]: warning: slow request (2.1 s)" ;;
  3) echo "2026-10-16T09:12:0$((i % 10))+0200 vps $name[$((1000 + i))]: error: connect() to unix:/run/php/php8.2-fpm.sock failed" ;;
  esac
  i=$((i + 1))
done
if [ -n "$follow" ]; then
  k=1
  while :; do sleep 1; echo "2026-10-16T09:13:$(printf %02d $((k % 60)))+0200 vps $name[1999]: live line $k"; k=$((k + 1)); done
fi
EOS
chmod 755 /usr/local/bin/systemctl /usr/local/bin/journalctl
