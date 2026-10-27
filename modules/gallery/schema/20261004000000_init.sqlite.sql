-- Albums are a collection (see package content).
CREATE TABLE wt_gallery_album (
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
    cover_id   TEXT,
    image_ids  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX wt_gallery_album_instance ON wt_gallery_album (instance);

CREATE TABLE wt_gallery_album_translation (
    item_id     TEXT NOT NULL REFERENCES wt_gallery_album (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    slug        TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);

CREATE INDEX wt_gallery_album_slug ON wt_gallery_album_translation (locale, slug);
