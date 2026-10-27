-- Events are revisioned content (see package content). Times are UTC; timezone
-- is the IANA zone the event is shown in.
CREATE TABLE wt_events_event (
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

CREATE INDEX wt_events_event_instance ON wt_events_event (instance);

CREATE TABLE wt_events_event_revision (
    id           TEXT PRIMARY KEY,
    entity_id    TEXT NOT NULL REFERENCES wt_events_event (id) ON DELETE CASCADE,
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
    starts_at    TIMESTAMP NOT NULL,
    ends_at      TIMESTAMP,
    all_day      BOOLEAN NOT NULL DEFAULT FALSE,
    timezone     TEXT NOT NULL DEFAULT 'UTC',
    image_id     TEXT,
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE wt_events_event_translation (
    revision_id      TEXT NOT NULL REFERENCES wt_events_event_revision (id) ON DELETE CASCADE,
    locale           TEXT NOT NULL,
    slug             TEXT NOT NULL DEFAULT '',
    title            TEXT NOT NULL DEFAULT '',
    summary          TEXT NOT NULL DEFAULT '',
    body             TEXT NOT NULL DEFAULT '',
    location_name    TEXT NOT NULL DEFAULT '',
    location_address TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (revision_id, locale)
);

CREATE INDEX wt_events_event_slug ON wt_events_event_translation (locale, slug);
