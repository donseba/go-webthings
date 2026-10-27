-- Posts are revisioned content, categories a collection (see package content).
CREATE TABLE wt_publishing_post (
    id                    TEXT PRIMARY KEY,
    instance              TEXT NOT NULL,
    published_revision_id TEXT,
    scheduled_revision_id TEXT,
    publish_at            TIMESTAMP,
    unpublish_at          TIMESTAMP,
    created_at            TIMESTAMP NOT NULL,
    created_by            TEXT,
    updated_at            TIMESTAMP NOT NULL,
    updated_by            TEXT,
    deleted_at            TIMESTAMP,
    deleted_by            TEXT
);

CREATE INDEX wt_publishing_post_instance ON wt_publishing_post (instance);

CREATE TABLE wt_publishing_post_revision (
    id           TEXT PRIMARY KEY,
    entity_id    TEXT NOT NULL REFERENCES wt_publishing_post (id) ON DELETE CASCADE,
    revision_no  INTEGER NOT NULL,
    author       TEXT,
    created_at   TIMESTAMP NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    -- The review workflow (see content.Review); the rest of a revision never changes.
    review_state TEXT NOT NULL DEFAULT 'draft',
    submitted_at TIMESTAMP,
    submitted_by TEXT,
    reviewed_at  TIMESTAMP,
    reviewed_by  TEXT,
    review_note  TEXT NOT NULL DEFAULT '',
    date         TIMESTAMP NOT NULL,
    image_id     TEXT,
    category_ids TEXT NOT NULL DEFAULT '',
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE wt_publishing_post_translation (
    revision_id TEXT NOT NULL REFERENCES wt_publishing_post_revision (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    slug        TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    excerpt     TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

CREATE INDEX wt_publishing_post_slug ON wt_publishing_post_translation (locale, slug);

CREATE TABLE wt_publishing_category (
    id         TEXT PRIMARY KEY,
    instance   TEXT NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    deleted_at TIMESTAMP,
    deleted_by TEXT
);

CREATE TABLE wt_publishing_category_translation (
    item_id TEXT NOT NULL REFERENCES wt_publishing_category (id) ON DELETE CASCADE,
    locale  TEXT NOT NULL,
    slug    TEXT NOT NULL DEFAULT '',
    title   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
