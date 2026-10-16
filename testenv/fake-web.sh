#!/bin/sh
# A small web page served on 127.0.0.1:3000 of the SSH test server (only
# the server itself reaches it), for the ports window: Open goes through a
# tunnel. Run as the SSH user, so that its program shows in netstat.
#   docker exec -i -u skiff testenv-ssh-1 sh -s start < testenv/fake-web.sh
#   docker exec -i -u skiff testenv-ssh-1 sh -s stop  < testenv/fake-web.sh
if [ "$1" = stop ]; then
  [ -f /tmp/fake-web.pid ] && kill "$(cat /tmp/fake-web.pid)" 2>/dev/null
  pkill -f "nc -N -l 127.0.0.1 3000" 2>/dev/null
  rm -f /tmp/fake-web.pid
  exit 0
fi
[ -f /tmp/fake-web.pid ] && kill "$(cat /tmp/fake-web.pid)" 2>/dev/null
pkill -f "nc -N -l 127.0.0.1 3000" 2>/dev/null
nohup sh -c 'while :; do printf "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nConnection: close\r\n\r\n<!doctype html><title>Tableau de bord</title><h1>Tableau de bord interne</h1>\n" | nc -N -l 127.0.0.1 3000 >/dev/null 2>&1; done' >/dev/null 2>&1 &
echo $! > /tmp/fake-web.pid
sleep 0.3
