-- One row per list entry; the instance is the list (see content.Collection).
CREATE TABLE wt_lists_entry (
    id         TEXT PRIMARY KEY,
    instance   TEXT NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    deleted_at TIMESTAMP,
    deleted_by TEXT,
    image_id   TEXT,
    link_url   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX wt_lists_entry_instance ON wt_lists_entry (instance, position);

CREATE TABLE wt_lists_entry_translation (
    item_id TEXT NOT NULL REFERENCES wt_lists_entry (id) ON DELETE CASCADE,
    locale  TEXT NOT NULL,
    title   TEXT NOT NULL DEFAULT '',
    body    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
