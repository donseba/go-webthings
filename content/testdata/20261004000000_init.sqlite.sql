-- Fixture schema for content tests. Media tables mirror the media package.
CREATE TABLE wt_media_asset (
    id         TEXT PRIMARY KEY,
    deleted_at TIMESTAMP
);

CREATE TABLE wt_media_usage (
    asset_id TEXT NOT NULL REFERENCES wt_media_asset (id),
    owner    TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    PRIMARY KEY (asset_id, owner, owner_id)
);

-- Revisioned content.
CREATE TABLE t_article (
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

CREATE TABLE t_article_revision (
    id           TEXT PRIMARY KEY,
    entity_id    TEXT NOT NULL REFERENCES t_article (id) ON DELETE CASCADE,
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
    pinned       BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE t_article_translation (
    revision_id TEXT NOT NULL REFERENCES t_article_revision (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    slug        TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

-- Collection content.
CREATE TABLE t_quote (
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
    author     TEXT NOT NULL DEFAULT ''
);

-- A unique field leaves deleted rows out, so a deleted quote does not keep
-- its author from new ones.
CREATE UNIQUE INDEX t_quote_author ON t_quote (instance, author) WHERE deleted_at IS NULL;

CREATE TABLE t_quote_translation (
    item_id TEXT NOT NULL REFERENCES t_quote (id) ON DELETE CASCADE,
    locale  TEXT NOT NULL,
    slug    TEXT NOT NULL DEFAULT '',
    text    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
