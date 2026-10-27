-- Fixture schema for the editor's tests: notes keep revisions, cards are a
-- collection (see package content).
CREATE TABLE t_note (
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

CREATE TABLE t_note_revision (
    id           TEXT PRIMARY KEY,
    entity_id    TEXT NOT NULL REFERENCES t_note (id) ON DELETE CASCADE,
    revision_no  INTEGER NOT NULL,
    author       TEXT,
    created_at   TIMESTAMP NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    review_state TEXT NOT NULL DEFAULT 'draft',
    submitted_at TIMESTAMP,
    submitted_by TEXT,
    reviewed_at  TIMESTAMP,
    reviewed_by  TEXT,
    review_note  TEXT NOT NULL DEFAULT '',
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE t_note_translation (
    revision_id TEXT NOT NULL REFERENCES t_note_revision (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

CREATE TABLE t_card (
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

CREATE TABLE t_card_translation (
    item_id TEXT NOT NULL REFERENCES t_card (id) ON DELETE CASCADE,
    locale  TEXT NOT NULL,
    body    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
