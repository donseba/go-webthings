-- One row per list entry; the instance is the list (see content.Collection).
CREATE TABLE wt_lists_entry (
    id         UUID PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    created_by UUID,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by UUID,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID,
    image_id   UUID,
    link_url   VARCHAR(2000) NOT NULL DEFAULT ''
);

CREATE INDEX wt_lists_entry_instance ON wt_lists_entry (instance, position);

CREATE TABLE wt_lists_entry_translation (
    item_id UUID NOT NULL REFERENCES wt_lists_entry (id) ON DELETE CASCADE,
    locale  VARCHAR(35) NOT NULL,
    title   VARCHAR(200) NOT NULL DEFAULT '',
    body    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
