-- Fixture schema for the editor's tests: notes keep revisions, cards are a
-- collection (see package content).
CREATE TABLE t_note (
    id                    UUID PRIMARY KEY,
    instance              VARCHAR(64) NOT NULL,
    published_revision_id UUID,
    scheduled_revision_id UUID,
    publish_at            TIMESTAMPTZ,
    unpublish_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL,
    created_by            UUID,
    updated_at            TIMESTAMPTZ NOT NULL,
    updated_by            UUID,
    deleted_at            TIMESTAMPTZ,
    deleted_by            UUID
);

CREATE TABLE t_note_revision (
    id           UUID PRIMARY KEY,
    entity_id    UUID NOT NULL REFERENCES t_note (id) ON DELETE CASCADE,
    revision_no  INTEGER NOT NULL,
    author       UUID,
    created_at   TIMESTAMPTZ NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    review_state VARCHAR(16) NOT NULL DEFAULT 'draft',
    submitted_at TIMESTAMPTZ,
    submitted_by UUID,
    reviewed_at  TIMESTAMPTZ,
    reviewed_by  UUID,
    review_note  VARCHAR(500) NOT NULL DEFAULT '',
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE t_note_translation (
    revision_id UUID NOT NULL REFERENCES t_note_revision (id) ON DELETE CASCADE,
    locale      VARCHAR(35) NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

CREATE TABLE t_card (
    id         UUID PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    created_by UUID,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by UUID,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID
);

CREATE TABLE t_card_translation (
    item_id UUID NOT NULL REFERENCES t_card (id) ON DELETE CASCADE,
    locale  VARCHAR(35) NOT NULL,
    body    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
