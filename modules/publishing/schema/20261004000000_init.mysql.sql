-- Posts are revisioned content, categories a collection (see package content).
CREATE TABLE wt_publishing_post (
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
    INDEX wt_publishing_post_instance (instance)
) ENGINE=InnoDB;

CREATE TABLE wt_publishing_post_revision (
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
    date         DATETIME(6) NOT NULL,
    image_id     VARCHAR(36) NULL,
    category_ids TEXT NOT NULL,
    UNIQUE (entity_id, revision_no),
    FOREIGN KEY (entity_id) REFERENCES wt_publishing_post (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE wt_publishing_post_translation (
    revision_id VARCHAR(36) NOT NULL,
    locale      VARCHAR(35) NOT NULL,
    slug        VARCHAR(190) NOT NULL DEFAULT '',
    title       VARCHAR(200) NOT NULL DEFAULT '',
    excerpt     TEXT NOT NULL,
    body        MEDIUMTEXT NOT NULL,
    PRIMARY KEY (revision_id, locale),
    INDEX wt_publishing_post_slug (locale, slug),
    FOREIGN KEY (revision_id) REFERENCES wt_publishing_post_revision (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE wt_publishing_category (
    id         VARCHAR(36) PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    deleted_at DATETIME(6) NULL,
    deleted_by VARCHAR(36) NULL
) ENGINE=InnoDB;

CREATE TABLE wt_publishing_category_translation (
    item_id VARCHAR(36) NOT NULL,
    locale  VARCHAR(35) NOT NULL,
    slug    VARCHAR(190) NOT NULL DEFAULT '',
    title   VARCHAR(200) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_publishing_category (id) ON DELETE CASCADE
) ENGINE=InnoDB;
