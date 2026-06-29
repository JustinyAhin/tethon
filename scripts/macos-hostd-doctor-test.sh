#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
tmp_home="$(mktemp -d)"
trap 'rm -rf "$tmp_home"' EXIT

export TETHON_HOSTD_HOME="$tmp_home"
source "$repo_root/scripts/macos-hostd.sh"

assert_eq() {
	local got="$1"
	local want="$2"
	local label="$3"
	if [[ "$got" != "$want" ]]; then
		echo "FAIL $label: got '$got', want '$want'" >&2
		exit 1
	fi
}

mkdir -p "$state_dir" "$config_dir"

cat >"$wrapper_path" <<'EOF'
#!/usr/bin/env bash
exec "$BIN" \
	-hub-url https://hub.tethon.dev \
	-addr 0.0.0.0:39181 \
	-repo-root /repos
EOF

assert_eq "$(doctor_value_from_wrapper "-hub-url")" "https://hub.tethon.dev" "wrapper hub url"
assert_eq "$(doctor_value_from_wrapper "-addr")" "0.0.0.0:39181" "wrapper addr"
assert_eq "$(doctor_http_base_for_addr "0.0.0.0:39181")" "http://127.0.0.1:39181" "wildcard addr"
assert_eq "$(doctor_http_base_for_addr "100.64.0.10:39181")" "http://100.64.0.10:39181" "concrete addr"

cat >"$config_file" <<'EOF'
TETHON_HUB_AUTH_TOKEN=secret
EOF
doctor_token_present

assert_eq "$(doctor_host_id_from_status '{"host_id":"mac"}')" "mac" "status host id"
assert_eq "$(doctor_host_online_from_hub '{"hosts":[{"host_id":"mac","online":true}]}' "mac")" "online" "hub online"
assert_eq "$(doctor_host_online_from_hub '{"hosts":[{"host_id":"mac","online":false}]}' "mac")" "offline" "hub offline"
assert_eq "$(doctor_host_online_from_hub '{"hosts":[]}' "mac")" "missing" "hub missing"

echo "PASS macos-hostd doctor helpers"
