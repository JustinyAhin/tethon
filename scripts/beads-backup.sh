#!/usr/bin/env bash
# Sync a Dolt-native Beads backup to a git-tracked directory.
# Restore (prefix: tethon): https://github.com/JustinyAhin/kb/blob/main/engineering/issue-tracking.md

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

BACKUP_DIR=".beads-backup"
BACKUP_PATH="$PWD/$BACKUP_DIR"

mkdir -p "$BACKUP_DIR"

# Configure or refresh the filesystem backup destination, then sync the
# full Dolt database state: tables, branches, commit history, and working set.
bd backup init "$BACKUP_PATH" -q
bd backup sync

# Remove legacy JSONL export artifacts from the old backup method. They are
# issue exports, not restore targets for bd 1.x.
rm -f \
  "$BACKUP_DIR/LOCK" \
  "$BACKUP_DIR/issues.jsonl" \
  "$BACKUP_DIR/dependencies.jsonl" \
  "$BACKUP_DIR/config.jsonl" \
  "$BACKUP_DIR/backup_state.json" \
  "$BACKUP_DIR/labels.jsonl" \
  "$BACKUP_DIR/events.jsonl" \
  "$BACKUP_DIR/comments.jsonl"

echo "Beads backup synced to $BACKUP_DIR/"
echo "Commit the changed files in $BACKUP_DIR/ to make the backup portable."
