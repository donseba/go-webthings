-- Categories and entries are collections (see package content). Slugs are not
-- translated and are unique per instance.
CREATE TABLE wt_directory_category (
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
    slug       TEXT NOT NULL
);

-- Deleted rows keep no slug, so a new item can take it.
CREATE UNIQUE INDEX wt_directory_category_slug ON wt_directory_category (instance, slug) WHERE deleted_at IS NULL;

CREATE TABLE wt_directory_category_translation (
    item_id TEXT NOT NULL REFERENCES wt_directory_category (id) ON DELETE CASCADE,
    locale  TEXT NOT NULL,
    name    TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);

-- A category with entries cannot be deleted: its entries would disappear from
-- the site. Editors move or delete them first.
CREATE TABLE wt_directory_entry (
    id            TEXT PRIMARY KEY,
    instance      TEXT NOT NULL,
    published     BOOLEAN NOT NULL DEFAULT FALSE,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL,
    created_by    TEXT,
    updated_at    TIMESTAMP NOT NULL,
    updated_by    TEXT,
    deleted_at    TIMESTAMP,
    deleted_by    TEXT,
    category_id   TEXT REFERENCES wt_directory_category (id),
    slug          TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    image_id      TEXT,
    website       TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL DEFAULT '',
    phone         TEXT NOT NULL DEFAULT '',
    street        TEXT NOT NULL DEFAULT '',
    postcode      TEXT NOT NULL DEFAULT '',
    city          TEXT NOT NULL DEFAULT '',
    latitude      REAL,
    longitude     REAL,
    opening_hours TEXT NOT NULL DEFAULT ''
);

-- Deleted rows keep no slug, so a new item can take it.
CREATE UNIQUE INDEX wt_directory_entry_slug ON wt_directory_entry (instance, slug) WHERE deleted_at IS NULL;

CREATE INDEX wt_directory_entry_category ON wt_directory_entry (category_id);

CREATE TABLE wt_directory_entry_translation (
    item_id     TEXT NOT NULL REFERENCES wt_directory_entry (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    keywords    TEXT NOT NULL DEFAULT '',
    hours_note  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
