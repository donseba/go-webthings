-- Albums are a collection (see package content).
CREATE TABLE wt_gallery_album (
    id         VARCHAR(36) PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    deleted_at DATETIME(6) NULL,
    deleted_by VARCHAR(36) NULL,
    cover_id   VARCHAR(36) NULL,
    image_ids  TEXT NOT NULL,
    INDEX wt_gallery_album_instance (instance)
) ENGINE=InnoDB;

CREATE TABLE wt_gallery_album_translation (
    item_id     VARCHAR(36) NOT NULL,
    locale      VARCHAR(35) NOT NULL,
    slug        VARCHAR(190) NOT NULL DEFAULT '',
    title       VARCHAR(200) NOT NULL DEFAULT '',
    description TEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    INDEX wt_gallery_album_slug (locale, slug),
    FOREIGN KEY (item_id) REFERENCES wt_gallery_album (id) ON DELETE CASCADE
) ENGINE=InnoDB;
