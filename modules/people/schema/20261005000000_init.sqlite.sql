-- People are a collection (see package content). The slug is the same in every
-- language and unique per instance.
CREATE TABLE wt_person (
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
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    photo_id   TEXT,
    email      TEXT NOT NULL DEFAULT '',
    phone      TEXT NOT NULL DEFAULT '',
    website    TEXT NOT NULL DEFAULT '',
    linkedin   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX wt_person_instance ON wt_person (instance, position);
-- Deleted rows keep no slug, so a new item can take it.
CREATE UNIQUE INDEX wt_person_slug ON wt_person (instance, slug) WHERE deleted_at IS NULL;

CREATE TABLE wt_person_translation (
    item_id    TEXT NOT NULL REFERENCES wt_person (id) ON DELETE CASCADE,
    locale     TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT '',
    role_title TEXT NOT NULL DEFAULT '',
    bio        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
