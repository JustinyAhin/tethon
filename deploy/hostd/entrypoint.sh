#!/bin/sh
set -eu

refresh_mode="${TETHON_HOSTD_REFRESH_REPOS:-strict}"
repo_roots="${TETHON_HOSTD_REPO_ROOTS:-/var/lib/tethon/repos}"

log() {
	printf '%s\n' "$*" >&2
}

fail() {
	log "repo refresh failed: $*"
	if [ "$refresh_mode" = "strict" ]; then
		exit 1
	fi
}

is_disabled() {
	case "$refresh_mode" in
		0|false|FALSE|no|NO|off|OFF)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

repo_top_level() {
	git -C "$1" rev-parse --show-toplevel 2>/dev/null || true
}

is_repo_root() {
	candidate="$1"
	top="$(repo_top_level "$candidate")"
	if [ -z "$top" ]; then
		return 1
	fi
	top="$(cd "$top" 2>/dev/null && pwd -P || true)"
	candidate="$(cd "$candidate" 2>/dev/null && pwd -P || true)"
	[ "$top" = "$candidate" ]
}

is_clean() {
	[ -z "$(git -C "$1" status --porcelain)" ]
}

default_branch() {
	repo="$1"
	branch="$(git -C "$repo" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null | sed 's#^origin/##' || true)"
	if [ -n "$branch" ]; then
		printf '%s\n' "$branch"
		return
	fi
	git -C "$repo" symbolic-ref --quiet --short HEAD 2>/dev/null || printf 'main\n'
}

switch_to_branch() {
	repo="$1"
	branch="$2"
	if git -C "$repo" show-ref --verify --quiet "refs/heads/$branch"; then
		git -C "$repo" switch "$branch"
	else
		git -C "$repo" switch --track -c "$branch" "origin/$branch"
	fi
}

refresh_repo() {
	repo="$1"
	if ! git -C "$repo" remote get-url origin >/dev/null 2>&1; then
		log "repo refresh skipped: $repo has no origin remote"
		return
	fi

	log "repo refresh: fetching $repo"
	if ! git -C "$repo" fetch --prune --tags origin; then
		fail "$repo fetch failed"
		return
	fi

	branch="$(default_branch "$repo")"
	current="$(git -C "$repo" symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
	if [ "$current" != "$branch" ]; then
		if ! is_clean "$repo"; then
			fail "$repo has local changes; refusing to switch from ${current:-detached} to $branch"
			return
		fi
		if ! switch_to_branch "$repo" "$branch"; then
			fail "$repo could not switch to $branch"
			return
		fi
	fi

	if ! is_clean "$repo"; then
		fail "$repo has local changes; refusing to fast-forward $branch"
		return
	fi
	if ! git -C "$repo" merge --ff-only "origin/$branch"; then
		fail "$repo could not fast-forward $branch from origin/$branch"
		return
	fi
}

refresh_root() {
	root="$1"
	if [ ! -d "$root" ]; then
		fail "$root does not exist"
		return
	fi
	if is_repo_root "$root"; then
		refresh_repo "$root"
		return
	fi
	found=0
	for candidate in "$root"/*; do
		[ -d "$candidate" ] || continue
		if is_repo_root "$candidate"; then
			found=1
			refresh_repo "$candidate"
		fi
	done
	if [ "$found" -eq 0 ]; then
		fail "$root contains no git repository roots"
	fi
}

if is_disabled; then
	log "repo refresh disabled"
else
	old_ifs="$IFS"
	IFS=:
	for root in $repo_roots; do
		IFS="$old_ifs"
		[ -n "$root" ] || continue
		refresh_root "$root"
		IFS=:
	done
	IFS="$old_ifs"
fi

exec /usr/local/bin/tethon-hostd "$@"
