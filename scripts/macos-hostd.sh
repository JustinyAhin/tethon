#!/usr/bin/env bash
set -euo pipefail

label="dev.tethon.hostd"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
control_dir="$repo_root/control"
home_dir="${TETHON_HOSTD_HOME:-$HOME}"

bin_dir="$home_dir/.local/bin"
bin_path="$bin_dir/tethon-hostd"
config_dir="$home_dir/.config/tethon"
config_file="$config_dir/hostd.env"
state_dir="$home_dir/Library/Application Support/Tethon/hostd"
log_dir="$home_dir/Library/Logs/Tethon"
wrapper_path="$state_dir/run-hostd"
plist_dir="$home_dir/Library/LaunchAgents"
plist_path="$plist_dir/$label.plist"
stdout_log="$log_dir/hostd.log"
stderr_log="$log_dir/hostd.err.log"

usage() {
	cat <<EOF
usage:
  scripts/macos-hostd.sh install --hub-url <url> --addr <host:port> --repo-root <path> [--repo-root <path> ...] [--host-id <id>] [--name <name>] [--allow-anthropic-api-key]
  scripts/macos-hostd.sh restart
  scripts/macos-hostd.sh status
  scripts/macos-hostd.sh logs
  scripts/macos-hostd.sh doctor
  scripts/macos-hostd.sh uninstall
EOF
}

quote() {
	printf "%q" "$1"
}

xml_escape() {
	local value="$1"
	value="${value//&/&amp;}"
	value="${value//</&lt;}"
	value="${value//>/&gt;}"
	value="${value//\"/&quot;}"
	printf "%s" "$value"
}

launch_domain() {
	printf "gui/%s" "$(id -u)"
}

service_target() {
	printf "%s/%s" "$(launch_domain)" "$label"
}

is_loaded() {
	launchctl print "$(service_target)" >/dev/null 2>&1
}

require_macos() {
	if [[ "$(uname -s)" != "Darwin" ]]; then
		echo "macos-hostd.sh only supports macOS launchd" >&2
		exit 1
	fi
}

ensure_config_file() {
	mkdir -p "$config_dir"
	if [[ ! -f "$config_file" ]]; then
		umask 077
		cat >"$config_file" <<EOF
# Tethon host daemon environment.
# Fill this in with the same token configured on the hub.
TETHON_HUB_AUTH_TOKEN=
EOF
	fi
	chmod 600 "$config_file"
}

write_wrapper() {
	local hub_url="$1"
	local addr="$2"
	local host_id="$3"
	local name="$4"
	local allow_anthropic_api_key="$5"
	shift 5
	local repo_roots=("$@")

	{
		echo "#!/usr/bin/env bash"
		echo "set -euo pipefail"
		echo "CONFIG_FILE=$(quote "$config_file")"
		echo "BIN=$(quote "$bin_path")"
		echo
		echo 'if [[ -f "$CONFIG_FILE" ]]; then'
		echo "	set -a"
		echo "	source \"\$CONFIG_FILE\""
		echo "	set +a"
		echo "fi"
		echo
		echo "exec \"\$BIN\" \\"
		echo "	-hub-url $(quote "$hub_url") \\"
		echo "	-addr $(quote "$addr") \\"
		echo "	-db $(quote "$state_dir/hostd.db") \\"
		echo "	-worktree-root $(quote "$state_dir/worktrees") \\"
		echo "	-cache-root $(quote "$state_dir/cache") \\"
		if [[ -n "$host_id" ]]; then
			echo "	-host-id $(quote "$host_id") \\"
		fi
		if [[ -n "$name" ]]; then
			echo "	-name $(quote "$name") \\"
		fi
		if [[ "$allow_anthropic_api_key" == "1" ]]; then
			echo "	-allow-anthropic-api-key \\"
		fi
		local repo_root
		for repo_root in "${repo_roots[@]}"; do
			echo "	-repo-root $(quote "$repo_root") \\"
		done
		echo "	\"\$@\""
	} >"$wrapper_path"
	chmod 755 "$wrapper_path"
}

write_plist() {
	cat >"$plist_path" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$(xml_escape "$label")</string>
	<key>ProgramArguments</key>
	<array>
		<string>$(xml_escape "$wrapper_path")</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>WorkingDirectory</key>
	<string>$(xml_escape "$state_dir")</string>
	<key>StandardOutPath</key>
	<string>$(xml_escape "$stdout_log")</string>
	<key>StandardErrorPath</key>
	<string>$(xml_escape "$stderr_log")</string>
</dict>
</plist>
EOF
}

build_binary() {
	mkdir -p "$bin_dir"
	(cd "$control_dir" && go build -o "$bin_path" ./cmd/tethon-hostd)
}

install_hostd() {
	require_macos

	local hub_url=""
	local addr=""
	local host_id=""
	local name=""
	local allow_anthropic_api_key="0"
	local repo_roots=()

	while [[ $# -gt 0 ]]; do
		case "$1" in
		--hub-url)
			hub_url="${2:-}"
			shift 2
			;;
		--addr)
			addr="${2:-}"
			shift 2
			;;
		--repo-root)
			repo_roots+=("${2:-}")
			shift 2
			;;
		--host-id)
			host_id="${2:-}"
			shift 2
			;;
		--name)
			name="${2:-}"
			shift 2
			;;
		--allow-anthropic-api-key)
			allow_anthropic_api_key="1"
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			echo "unknown install option: $1" >&2
			usage >&2
			exit 1
			;;
		esac
	done

	if [[ -z "$hub_url" || -z "$addr" || ${#repo_roots[@]} -eq 0 ]]; then
		echo "install requires --hub-url, --addr, and at least one --repo-root" >&2
		usage >&2
		exit 1
	fi

	mkdir -p "$state_dir" "$log_dir" "$plist_dir"
	ensure_config_file
	build_binary
	write_wrapper "$hub_url" "$addr" "$host_id" "$name" "$allow_anthropic_api_key" "${repo_roots[@]}"
	write_plist

	if is_loaded; then
		launchctl bootout "$(service_target)" >/dev/null 2>&1 || true
	fi
	launchctl bootstrap "$(launch_domain)" "$plist_path"
	launchctl kickstart -k "$(service_target)"

	echo "Installed $label"
	echo "Config: $config_file"
	echo "Logs: $stdout_log and $stderr_log"
}

restart_hostd() {
	require_macos
	if is_loaded; then
		launchctl kickstart -k "$(service_target)"
	else
		launchctl bootstrap "$(launch_domain)" "$plist_path"
		launchctl kickstart -k "$(service_target)"
	fi
}

status_hostd() {
	require_macos
	launchctl print "$(service_target)"
}

logs_hostd() {
	mkdir -p "$log_dir"
	touch "$stdout_log" "$stderr_log"
	tail -n 100 -f "$stdout_log" "$stderr_log"
}

doctor_passes=0
doctor_warnings=0
doctor_failures=0

doctor_pass() {
	doctor_passes=$((doctor_passes + 1))
	printf "PASS %s\n" "$1"
}

doctor_warn() {
	doctor_warnings=$((doctor_warnings + 1))
	printf "WARN %s\n" "$1"
}

doctor_fail() {
	doctor_failures=$((doctor_failures + 1))
	printf "FAIL %s\n" "$1"
}

doctor_value_from_wrapper() {
	local flag="$1"
	if [[ ! -f "$wrapper_path" ]]; then
		return 1
	fi
	awk -v flag="$flag" '$1 == flag { print $2; exit }' "$wrapper_path"
}

doctor_http_base_for_addr() {
	local addr="$1"
	local host="${addr%:*}"
	local port="${addr##*:}"
	if [[ "$host" == "$addr" ]]; then
		return 1
	fi
	if [[ -z "$host" || "$host" == "0.0.0.0" || "$host" == "::" ]]; then
		host="127.0.0.1"
	fi
	printf "http://%s:%s" "$host" "$port"
}

doctor_token_present() {
	if [[ ! -f "$config_file" ]]; then
		return 1
	fi
	grep -Eq '^[[:space:]]*TETHON_HUB_AUTH_TOKEN=[^[:space:]#]+' "$config_file"
}

doctor_host_id_from_status() {
	local status_json="$1"
	sed -n 's/.*"host_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' <<<"$status_json" | head -n 1
}

doctor_host_online_from_hub() {
	local hosts_json="$1"
	local host_id="$2"
	local compact="${hosts_json//$'\n'/}"
	if [[ "$compact" != *"\"host_id\":\"$host_id\""* ]]; then
		printf "missing"
		return
	fi
	local tail="${compact#*"\"host_id\":\"$host_id\""}"
	local host_entry="${tail%%\}*}"
	if [[ "$host_entry" == *'"online":true'* ]]; then
		printf "online"
	else
		printf "offline"
	fi
}

doctor_hostd() {
	require_macos

	local addr=""
	local hub_url=""
	local local_base=""
	local status_json=""
	local host_id=""

	if [[ -f "$plist_path" ]]; then
		doctor_pass "LaunchAgent plist exists: $plist_path"
	else
		doctor_fail "LaunchAgent plist is missing: $plist_path"
	fi

	if [[ -x "$wrapper_path" ]]; then
		doctor_pass "LaunchAgent wrapper exists: $wrapper_path"
	else
		doctor_fail "LaunchAgent wrapper is missing or not executable: $wrapper_path"
	fi

	if is_loaded; then
		doctor_pass "launchd service is loaded: $(service_target)"
	else
		doctor_fail "launchd service is not loaded: $(service_target)"
	fi

	addr="$(doctor_value_from_wrapper "-addr" || true)"
	hub_url="$(doctor_value_from_wrapper "-hub-url" || true)"
	if [[ -n "$addr" ]]; then
		doctor_pass "Configured hostd listener: $addr"
		local_base="$(doctor_http_base_for_addr "$addr" || true)"
	else
		doctor_fail "Could not read -addr from wrapper"
	fi
	if [[ -n "$hub_url" ]]; then
		doctor_pass "Configured hub URL: $hub_url"
	else
		doctor_fail "Could not read -hub-url from wrapper"
	fi

	local process_lines=""
	process_lines="$(pgrep -fl "[t]ethon-hostd" 2>/dev/null || true)"
	if [[ -z "$process_lines" ]]; then
		doctor_warn "No tethon-hostd process found"
	else
		local process_count
		process_count="$(printf "%s\n" "$process_lines" | sed '/^[[:space:]]*$/d' | wc -l | tr -d ' ')"
		if [[ "$process_count" -eq 1 ]]; then
			doctor_pass "One tethon-hostd process is running"
		else
			doctor_warn "Multiple tethon-hostd processes found; check for stale manual daemons"
			printf "%s\n" "$process_lines" | sed 's/^/     /'
		fi
	fi

	if [[ -n "$local_base" ]]; then
		if curl -fsS --max-time 2 "$local_base/healthz" >/dev/null 2>&1; then
			doctor_pass "Local hostd /healthz is reachable: $local_base/healthz"
		else
			doctor_fail "Local hostd /healthz is not reachable: $local_base/healthz"
		fi
		status_json="$(curl -fsS --max-time 2 "$local_base/status" 2>/dev/null || true)"
		if [[ -n "$status_json" ]]; then
			doctor_pass "Local hostd /status is reachable: $local_base/status"
			host_id="$(doctor_host_id_from_status "$status_json" || true)"
			if [[ -n "$host_id" ]]; then
				doctor_pass "Local hostd reports host id: $host_id"
			else
				doctor_warn "Local hostd /status did not include host_id"
			fi
		else
			doctor_warn "Local hostd /status is not reachable: $local_base/status"
		fi
	fi

	if [[ -f "$stdout_log" || -f "$stderr_log" ]]; then
		doctor_pass "Hostd log files exist in $log_dir"
	else
		doctor_warn "Hostd log files do not exist yet in $log_dir"
	fi
	if [[ -s "$stderr_log" ]]; then
		doctor_warn "Recent stderr log output exists; last 10 lines:"
		tail -n 10 "$stderr_log" | sed 's/^/     /'
	fi

	if [[ -n "$hub_url" ]]; then
		if curl -fsS --max-time 5 "${hub_url%/}/healthz" >/dev/null 2>&1; then
			doctor_pass "Hub /healthz is reachable: ${hub_url%/}/healthz"
		else
			doctor_fail "Hub /healthz is not reachable: ${hub_url%/}/healthz"
		fi
	fi

	if doctor_token_present; then
		doctor_pass "Hub token is present in $config_file"
	else
		doctor_warn "Hub token is missing or empty in $config_file"
	fi

	if [[ -n "$hub_url" && -n "$host_id" ]] && doctor_token_present; then
		local hosts_json=""
		hosts_json="$(set -a; source "$config_file"; set +a; curl -fsS --max-time 5 -H "Authorization: Bearer $TETHON_HUB_AUTH_TOKEN" "${hub_url%/}/hosts" 2>/dev/null || true)"
		if [[ -z "$hosts_json" ]]; then
			doctor_warn "Could not query authenticated hub /hosts"
		else
			local online_state
			online_state="$(doctor_host_online_from_hub "$hosts_json" "$host_id" || true)"
			case "$online_state" in
			online)
				doctor_pass "Hub reports host online: $host_id"
				;;
			offline)
				doctor_fail "Hub knows host but reports it offline: $host_id"
				;;
			missing | "")
				doctor_fail "Hub does not list host: $host_id"
				;;
			esac
		fi
	fi

	printf "\nSummary: %d passed, %d warnings, %d failures\n" "$doctor_passes" "$doctor_warnings" "$doctor_failures"
	if [[ "$doctor_failures" -gt 0 ]]; then
		return 1
	fi
}

uninstall_hostd() {
	require_macos
	if is_loaded; then
		launchctl bootout "$(service_target)" >/dev/null 2>&1 || true
	fi
	rm -f "$plist_path" "$wrapper_path" "$bin_path"
	echo "Uninstalled $label"
	echo "State, config, and logs were left in place:"
	echo "  $state_dir"
	echo "  $config_file"
	echo "  $log_dir"
}

main() {
	local command="${1:-}"
	if [[ -z "$command" ]]; then
		usage >&2
		exit 1
	fi
	shift

	case "$command" in
	install)
		install_hostd "$@"
		;;
	restart)
		restart_hostd "$@"
		;;
	status)
		status_hostd "$@"
		;;
	logs)
		logs_hostd "$@"
		;;
	doctor)
		doctor_hostd "$@"
		;;
	uninstall)
		uninstall_hostd "$@"
		;;
	-h | --help)
		usage
		;;
	*)
		echo "unknown command: $command" >&2
		usage >&2
		exit 1
		;;
	esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	main "$@"
fi
