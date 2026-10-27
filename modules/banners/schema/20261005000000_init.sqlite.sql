-- Banners are a collection (see package content). Times are UTC.
CREATE TABLE wt_banner (
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
    tone       TEXT NOT NULL DEFAULT 'info',
    starts_at  TIMESTAMP,
    ends_at    TIMESTAMP
);

CREATE INDEX wt_banner_instance ON wt_banner (instance, position);

CREATE TABLE wt_banner_translation (
    item_id      TEXT NOT NULL REFERENCES wt_banner (id) ON DELETE CASCADE,
    locale       TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    subtitle     TEXT NOT NULL DEFAULT '',
    body         TEXT NOT NULL DEFAULT '',
    button_label TEXT NOT NULL DEFAULT '',
    button_url   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
