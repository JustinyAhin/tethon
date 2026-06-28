#!/usr/bin/env bash
# Export beads issues to git-tracked backup directory.
# Run manually or via git pre-commit hook.
# Restore with: bd backup restore .beads-backup/

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

BACKUP_DIR=".beads-backup"

mkdir -p "$BACKUP_DIR"

# Run bd backup to refresh .beads/backup/
bd backup --force -q 2>/dev/null

# Copy to git-tracked location
cp .beads/backup/*.jsonl "$BACKUP_DIR/"
cp .beads/backup/backup_state.json "$BACKUP_DIR/"

echo "Beads backup exported to $BACKUP_DIR/ ($(wc -l < "$BACKUP_DIR/issues.jsonl") issues)"
