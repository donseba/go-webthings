-- Posts are revisioned content, categories a collection (see package content).
CREATE TABLE wt_publishing_post (
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

CREATE INDEX wt_publishing_post_instance ON wt_publishing_post (instance);

CREATE TABLE wt_publishing_post_revision (
    id           UUID PRIMARY KEY,
    entity_id    UUID NOT NULL REFERENCES wt_publishing_post (id) ON DELETE CASCADE,
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
    date         TIMESTAMPTZ NOT NULL,
    image_id     UUID,
    category_ids TEXT NOT NULL DEFAULT '',
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE wt_publishing_post_translation (
    revision_id UUID NOT NULL REFERENCES wt_publishing_post_revision (id) ON DELETE CASCADE,
    locale      VARCHAR(35) NOT NULL,
    slug        VARCHAR(190) NOT NULL DEFAULT '',
    title       VARCHAR(200) NOT NULL DEFAULT '',
    excerpt     TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

CREATE INDEX wt_publishing_post_slug ON wt_publishing_post_translation (locale, slug);

CREATE TABLE wt_publishing_category (
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

CREATE TABLE wt_publishing_category_translation (
    item_id UUID NOT NULL REFERENCES wt_publishing_category (id) ON DELETE CASCADE,
    locale  VARCHAR(35) NOT NULL,
    slug    VARCHAR(190) NOT NULL DEFAULT '',
    title   VARCHAR(200) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
