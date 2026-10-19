#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
tmp_home="$(mktemp -d)"
trap 'rm -rf "$tmp_home"' EXIT

export TETHON_HOSTD_HOME="$tmp_home"
source "$repo_root/scripts/macos-hostd.sh"

require_macos() { :; }
build_binary() { :; }

service_disabled=1
service_loaded=0
service_running=0

launchctl() {
	case "$1" in
	print)
		[[ "$service_loaded" == 1 ]]
		;;
	enable)
		service_disabled=0
		;;
	bootstrap)
		if [[ "$service_disabled" == 1 ]]; then
			echo "Bootstrap failed: service is disabled" >&2
			return 1
		fi
		service_loaded=1
		;;
	bootout)
		service_loaded=0
		service_running=0
		;;
	kickstart)
		[[ "$service_loaded" == 1 ]] || return 1
		service_running=1
		;;
	*)
		echo "Unexpected launchctl command: $1" >&2
		return 1
		;;
	esac
}

assert_running() {
	if [[ "$service_disabled" != 0 || "$service_loaded" != 1 || "$service_running" != 1 ]]; then
		echo "FAIL $1: service is not enabled, loaded, and running" >&2
		exit 1
	fi
}

install_hostd --hub-url https://hub.tethon.dev --addr 127.0.0.1:39181 --repo-root "$repo_root" >/dev/null
assert_running "install disabled service"

service_disabled=1
service_loaded=0
service_running=0
restart_hostd
assert_running "restart disabled, unloaded service"

service_disabled=1
service_running=0
restart_hostd
assert_running "restart disabled, loaded service"

echo "PASS macos-hostd lifecycle"
