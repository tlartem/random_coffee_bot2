-- Added by us: store the poll message_id for pin/unpin logic

ALTER TABLE poll_mapping
ADD COLUMN message_id INTEGER NOT NULL DEFAULT 0;
