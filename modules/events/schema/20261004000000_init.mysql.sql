-- Events are revisioned content (see package content). Times are UTC; timezone
-- is the IANA zone the event is shown in.
CREATE TABLE wt_events_event (
    id                    VARCHAR(36) PRIMARY KEY,
    instance              VARCHAR(64) NOT NULL,
    published_revision_id VARCHAR(36) NULL,
    scheduled_revision_id VARCHAR(36) NULL,
    publish_at            DATETIME(6) NULL,
    unpublish_at          DATETIME(6) NULL,
    created_at            DATETIME(6) NOT NULL,
    created_by            VARCHAR(36) NULL,
    updated_at            DATETIME(6) NOT NULL,
    updated_by            VARCHAR(36) NULL,
    deleted_at            DATETIME(6) NULL,
    deleted_by            VARCHAR(36) NULL,
    INDEX wt_events_event_instance (instance)
) ENGINE=InnoDB;

CREATE TABLE wt_events_event_revision (
    id           VARCHAR(36) PRIMARY KEY,
    entity_id    VARCHAR(36) NOT NULL,
    revision_no  INTEGER NOT NULL,
    author       VARCHAR(36) NULL,
    created_at   DATETIME(6) NOT NULL,
    note         TEXT NOT NULL,
    -- The review workflow (see content.Review); the rest of a revision never changes.
    review_state VARCHAR(16) NOT NULL DEFAULT 'draft',
    submitted_at DATETIME(6) NULL,
    submitted_by VARCHAR(36) NULL,
    reviewed_at  DATETIME(6) NULL,
    reviewed_by  VARCHAR(36) NULL,
    review_note  VARCHAR(500) NOT NULL DEFAULT '',
    starts_at    DATETIME(6) NOT NULL,
    ends_at      DATETIME(6) NULL,
    all_day      BOOLEAN NOT NULL DEFAULT FALSE,
    timezone     VARCHAR(100) NOT NULL DEFAULT 'UTC',
    image_id     VARCHAR(36) NULL,
    UNIQUE (entity_id, revision_no),
    FOREIGN KEY (entity_id) REFERENCES wt_events_event (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE wt_events_event_translation (
    revision_id      VARCHAR(36) NOT NULL,
    locale           VARCHAR(35) NOT NULL,
    slug             VARCHAR(190) NOT NULL DEFAULT '',
    title            VARCHAR(200) NOT NULL DEFAULT '',
    summary          TEXT NOT NULL,
    body             MEDIUMTEXT NOT NULL,
    location_name    VARCHAR(200) NOT NULL DEFAULT '',
    location_address TEXT NOT NULL,
    PRIMARY KEY (revision_id, locale),
    INDEX wt_events_event_slug (locale, slug),
    FOREIGN KEY (revision_id) REFERENCES wt_events_event_revision (id) ON DELETE CASCADE
) ENGINE=InnoDB;
