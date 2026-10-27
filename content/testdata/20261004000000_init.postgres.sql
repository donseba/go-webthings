-- Fixture schema for content tests. Media tables mirror the media package.
CREATE TABLE wt_media_asset (
    id         UUID PRIMARY KEY,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE wt_media_usage (
    asset_id UUID NOT NULL REFERENCES wt_media_asset (id),
    owner    VARCHAR(100) NOT NULL,
    owner_id VARCHAR(100) NOT NULL,
    PRIMARY KEY (asset_id, owner, owner_id)
);

-- Revisioned content.
CREATE TABLE t_article (
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

CREATE TABLE t_article_revision (
    id           UUID PRIMARY KEY,
    entity_id    UUID NOT NULL REFERENCES t_article (id) ON DELETE CASCADE,
    revision_no  INTEGER NOT NULL,
    author       UUID,
    created_at   TIMESTAMPTZ NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    -- The review workflow (see content.Review); the rest of a revision never changes.
    review_state VARCHAR(16) NOT NULL DEFAULT 'draft',
    submitted_at TIMESTAMPTZ,
    submitted_by UUID,
    reviewed_at  TIMESTAMPTZ,
    reviewed_by  UUID,
    review_note  VARCHAR(500) NOT NULL DEFAULT '',
    pinned       BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE t_article_translation (
    revision_id UUID NOT NULL REFERENCES t_article_revision (id) ON DELETE CASCADE,
    locale      VARCHAR(35) NOT NULL,
    slug        VARCHAR(190) NOT NULL DEFAULT '',
    title       VARCHAR(200) NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

-- Collection content.
CREATE TABLE t_quote (
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
    author     VARCHAR(200) NOT NULL DEFAULT ''
);

-- A unique field leaves deleted rows out, so a deleted quote does not keep
-- its author from new ones.
CREATE UNIQUE INDEX t_quote_author ON t_quote (instance, author) WHERE deleted_at IS NULL;

CREATE TABLE t_quote_translation (
    item_id UUID NOT NULL REFERENCES t_quote (id) ON DELETE CASCADE,
    locale  VARCHAR(35) NOT NULL,
    slug    VARCHAR(190) NOT NULL DEFAULT '',
    text    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
