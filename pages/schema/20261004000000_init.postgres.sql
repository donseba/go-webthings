-- Pages are revisioned content (see package content) plus a route table that
-- is rebuilt from the published tree on every publish.
CREATE TABLE wt_page (
    id                    UUID PRIMARY KEY,
    instance              VARCHAR(64) NOT NULL,
    published_revision_id UUID,
    scheduled_revision_id UUID,
    publish_at            TIMESTAMPTZ,
    unpublish_at          TIMESTAMPTZ,
    -- position orders a page among its siblings; it is not revisioned.
    position              INTEGER NOT NULL DEFAULT 0,
    -- published_position is the published revision's place while the latest
    -- revision has another parent, until that is published; NULL otherwise.
    published_position    INTEGER,
    created_at            TIMESTAMPTZ NOT NULL,
    created_by            UUID,
    updated_at            TIMESTAMPTZ NOT NULL,
    updated_by            UUID,
    deleted_at            TIMESTAMPTZ,
    deleted_by            UUID
);

CREATE TABLE wt_page_revision (
    id           UUID PRIMARY KEY,
    entity_id    UUID NOT NULL REFERENCES wt_page (id) ON DELETE CASCADE,
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
    parent_id    UUID,
    module       VARCHAR(64) NOT NULL DEFAULT '',
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE wt_page_translation (
    revision_id UUID NOT NULL REFERENCES wt_page_revision (id) ON DELETE CASCADE,
    locale      VARCHAR(35) NOT NULL,
    slug        VARCHAR(190) NOT NULL DEFAULT '',
    title       VARCHAR(200) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

CREATE TABLE wt_page_route (
    locale      VARCHAR(35) NOT NULL,
    path        VARCHAR(700) NOT NULL,
    page_id     UUID NOT NULL,
    revision_id UUID NOT NULL,
    parent_id   VARCHAR(36) NOT NULL DEFAULT '',
    module      VARCHAR(64) NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    depth       INTEGER NOT NULL DEFAULT 0,
    title       VARCHAR(200) NOT NULL DEFAULT '',
    PRIMARY KEY (locale, path)
);

CREATE INDEX wt_page_route_page ON wt_page_route (page_id);
