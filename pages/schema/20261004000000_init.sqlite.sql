-- Pages are revisioned content (see package content) plus a route table that
-- is rebuilt from the published tree on every publish.
CREATE TABLE wt_page (
    id                    TEXT PRIMARY KEY,
    instance              TEXT NOT NULL,
    published_revision_id TEXT,
    scheduled_revision_id TEXT,
    publish_at            TIMESTAMP,
    unpublish_at          TIMESTAMP,
    -- position orders a page among its siblings; it is not revisioned.
    position              INTEGER NOT NULL DEFAULT 0,
    -- published_position is the published revision's place while the latest
    -- revision has another parent, until that is published; NULL otherwise.
    published_position    INTEGER,
    created_at            TIMESTAMP NOT NULL,
    created_by            TEXT,
    updated_at            TIMESTAMP NOT NULL,
    updated_by            TEXT,
    deleted_at            TIMESTAMP,
    deleted_by            TEXT
);

CREATE TABLE wt_page_revision (
    id           TEXT PRIMARY KEY,
    entity_id    TEXT NOT NULL REFERENCES wt_page (id) ON DELETE CASCADE,
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
    parent_id    TEXT,
    module       TEXT NOT NULL DEFAULT '',
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE wt_page_translation (
    revision_id TEXT NOT NULL REFERENCES wt_page_revision (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    slug        TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

CREATE TABLE wt_page_route (
    locale      TEXT NOT NULL,
    path        TEXT NOT NULL,
    page_id     TEXT NOT NULL,
    revision_id TEXT NOT NULL,
    parent_id   TEXT NOT NULL DEFAULT '',
    module      TEXT NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    depth       INTEGER NOT NULL DEFAULT 0,
    title       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (locale, path)
);

CREATE INDEX wt_page_route_page ON wt_page_route (page_id);
