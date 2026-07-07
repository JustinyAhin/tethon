#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: dokploy-set-env.sh <base-url> <api-key> <environment-id> <name> <value>" >&2
}

if [ "$#" -ne 5 ]; then
  usage
  exit 2
fi

base_url="${1%/}"
api_key="$2"
environment_id="$3"
name="$4"
value="$5"

if [ -z "$base_url" ] || [ -z "$api_key" ] || [ -z "$environment_id" ] || [ -z "$name" ]; then
  usage
  exit 2
fi

current_json="$(mktemp)"
updated_env_file="$(mktemp)"
trap 'rm -f "$current_json" "$updated_env_file"' EXIT

curl --fail-with-body --silent --show-error --retry 3 --retry-delay 5 \
  --get "$base_url/api/environment.one" \
  --header "x-api-key: $api_key" \
  --data-urlencode "environmentId=$environment_id" \
  --output "$current_json"

jq -r '
  if has("env") then
    .env // ""
  elif (.environment? | type) == "object" then
    .environment.env // ""
  else
    error("Dokploy environment response did not include env")
  end
' "$current_json" | awk -v name="$name" -v value="$value" '
  BEGIN { found = 0 }
  $0 ~ "^" name "=" {
    print name "=" value
    found = 1
    next
  }
  { print }
  END {
    if (!found) print name "=" value
  }
' > "$updated_env_file"

updated_env="$(cat "$updated_env_file")"

curl --fail-with-body --silent --show-error --retry 3 --retry-delay 5 \
  --request POST "$base_url/api/environment.update" \
  --header "x-api-key: $api_key" \
  --header "Content-Type: application/json" \
  --data "$(jq -n --arg environmentId "$environment_id" --arg env "$updated_env" '{environmentId: $environmentId, env: $env}')"

echo "Updated Dokploy environment variable $name."
