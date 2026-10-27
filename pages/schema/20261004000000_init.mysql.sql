-- Pages are revisioned content (see package content) plus a route table that
-- is rebuilt from the published tree on every publish.
CREATE TABLE wt_page (
    id                    VARCHAR(36) PRIMARY KEY,
    instance              VARCHAR(64) NOT NULL,
    published_revision_id VARCHAR(36) NULL,
    scheduled_revision_id VARCHAR(36) NULL,
    publish_at            DATETIME(6) NULL,
    unpublish_at          DATETIME(6) NULL,
    -- position orders a page among its siblings; it is not revisioned.
    position              INTEGER NOT NULL DEFAULT 0,
    -- published_position is the published revision's place while the latest
    -- revision has another parent, until that is published; NULL otherwise.
    published_position    INTEGER,
    created_at            DATETIME(6) NOT NULL,
    created_by            VARCHAR(36) NULL,
    updated_at            DATETIME(6) NOT NULL,
    updated_by            VARCHAR(36) NULL,
    deleted_at            DATETIME(6) NULL,
    deleted_by            VARCHAR(36) NULL
) ENGINE=InnoDB;

CREATE TABLE wt_page_revision (
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
    parent_id    VARCHAR(36),
    module       VARCHAR(64) NOT NULL DEFAULT '',
    UNIQUE (entity_id, revision_no),
    FOREIGN KEY (entity_id) REFERENCES wt_page (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE wt_page_translation (
    revision_id VARCHAR(36) NOT NULL,
    locale      VARCHAR(35) NOT NULL,
    slug        VARCHAR(190) NOT NULL DEFAULT '',
    title       VARCHAR(200) NOT NULL DEFAULT '',
    description TEXT NOT NULL,
    body        MEDIUMTEXT NOT NULL,
    PRIMARY KEY (revision_id, locale),
    FOREIGN KEY (revision_id) REFERENCES wt_page_revision (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE wt_page_route (
    locale      VARCHAR(35) NOT NULL,
    path        VARCHAR(700) NOT NULL,
    page_id     VARCHAR(36) NOT NULL,
    revision_id VARCHAR(36) NOT NULL,
    parent_id   VARCHAR(36) NOT NULL DEFAULT '',
    module      VARCHAR(64) NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    depth       INTEGER NOT NULL DEFAULT 0,
    title       VARCHAR(200) NOT NULL DEFAULT '',
    PRIMARY KEY (locale, path),
    INDEX wt_page_route_page (page_id)
) ENGINE=InnoDB;
