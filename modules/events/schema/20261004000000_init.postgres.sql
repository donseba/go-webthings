-- Events are revisioned content (see package content). Times are UTC; timezone
-- is the IANA zone the event is shown in.
CREATE TABLE wt_events_event (
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

CREATE INDEX wt_events_event_instance ON wt_events_event (instance);

CREATE TABLE wt_events_event_revision (
    id           UUID PRIMARY KEY,
    entity_id    UUID NOT NULL REFERENCES wt_events_event (id) ON DELETE CASCADE,
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
    starts_at    TIMESTAMPTZ NOT NULL,
    ends_at      TIMESTAMPTZ,
    all_day      BOOLEAN NOT NULL DEFAULT FALSE,
    timezone     VARCHAR(100) NOT NULL DEFAULT 'UTC',
    image_id     UUID,
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE wt_events_event_translation (
    revision_id      UUID NOT NULL REFERENCES wt_events_event_revision (id) ON DELETE CASCADE,
    locale           VARCHAR(35) NOT NULL,
    slug             VARCHAR(190) NOT NULL DEFAULT '',
    title            VARCHAR(200) NOT NULL DEFAULT '',
    summary          TEXT NOT NULL DEFAULT '',
    body             TEXT NOT NULL DEFAULT '',
    location_name    VARCHAR(200) NOT NULL DEFAULT '',
    location_address TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

CREATE INDEX wt_events_event_slug ON wt_events_event_translation (locale, slug);
