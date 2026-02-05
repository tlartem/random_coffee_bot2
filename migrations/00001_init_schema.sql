-- Initial tables (ported from Alembic revision 51089e76be28)
-- Created: 2025-11-04 14:43:35.891617

CREATE TABLE IF NOT EXISTS pair (
  id TEXT PRIMARY KEY,
  group_id INTEGER NOT NULL,
  week_start TEXT NOT NULL,
  user1_id INTEGER NOT NULL,
  user2_id INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  CONSTRAINT uix_pairs_week_user UNIQUE (group_id, week_start, user1_id, user2_id)
);

CREATE TABLE IF NOT EXISTS participant (
  id TEXT PRIMARY KEY,
  group_id INTEGER NOT NULL,
  user_id INTEGER NOT NULL,
  username TEXT NOT NULL,
  full_name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  CONSTRAINT uix_participants_group_user UNIQUE (group_id, user_id)
);

CREATE TABLE IF NOT EXISTS poll_mapping (
  poll_id TEXT PRIMARY KEY,
  group_id INTEGER NOT NULL
);
