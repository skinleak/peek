#!/bin/sh
# Runs inside the VHS container (see record.sh): stages the dev servers that
# peek shows in the demo. Each is a copy of fakeserver named like the real
# program, started from a project directory with realistic arguments.
set -e
bin=/demo/bin
for name in node python3 redis-server postgres; do
	cp "$bin/fakeserver" "/usr/local/bin/$name"
done
cp "$bin/peek" /usr/local/bin/peek

# The servers, and peek itself (see demo.tape), run as an ordinary user.
id dev >/dev/null 2>&1 || useradd --uid 1000 --no-create-home --home-dir /home/dev dev
export HOME=/home/dev
mkdir -p ~/code/webapp/client ~/code/api /var/lib/redis /var/lib/postgresql/data

start() { # start <dir> <listen> <clients> <command...>
	dir=$1 listen=$2 clients=$3
	shift 3
	(cd "$dir" && DEMO_LISTEN=$listen DEMO_CLIENTS=$clients setsid setpriv --reuid=dev --regid=dev --init-groups "$@" >/dev/null 2>&1 &)
}
start /var/lib/postgresql/data 0.0.0.0:5432 2 postgres -D /var/lib/postgresql/data
start /var/lib/redis 127.0.0.1:6379,[::1]:6379 1 redis-server /etc/redis/redis.conf
start ~/code/webapp 127.0.0.1:3000,[::1]:3000 3 node ~/code/webapp/node_modules/.bin/next dev
start ~/code/webapp/client [::1]:5173 0 node ~/code/webapp/client/node_modules/vite/bin/vite.js
start ~/code/api 0.0.0.0:8000 0 python3 manage.py runserver 0.0.0.0:8000
# A server that comes up while the demo is running, to show the + marker.
(sleep "${LATE_START:-9}" && start ~/code/webapp 127.0.0.1:6006 0 node ~/code/webapp/node_modules/.bin/storybook dev) &
sleep 0.5
