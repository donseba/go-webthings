-- Banners are a collection (see package content). Times are UTC.
CREATE TABLE wt_banner (
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
    image_id   VARCHAR(36) NULL,
    tone       VARCHAR(20) NOT NULL DEFAULT 'info',
    starts_at  DATETIME(6) NULL,
    ends_at    DATETIME(6) NULL,
    INDEX wt_banner_instance (instance, position)
) ENGINE=InnoDB;

CREATE TABLE wt_banner_translation (
    item_id      VARCHAR(36) NOT NULL,
    locale       VARCHAR(35) NOT NULL,
    title        VARCHAR(200) NOT NULL DEFAULT '',
    subtitle     VARCHAR(500) NOT NULL DEFAULT '',
    body         TEXT NOT NULL,
    button_label VARCHAR(100) NOT NULL DEFAULT '',
    button_url   VARCHAR(2000) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_banner (id) ON DELETE CASCADE
) ENGINE=InnoDB;
