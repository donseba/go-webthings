-- Testimonials are a collection (see package content).
CREATE TABLE wt_testimonial (
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
    photo_id   VARCHAR(36) NULL,
    author     VARCHAR(200) NOT NULL DEFAULT '',
    INDEX wt_testimonial_instance (instance, position)
) ENGINE=InnoDB;

CREATE TABLE wt_testimonial_translation (
    item_id      VARCHAR(36) NOT NULL,
    locale       VARCHAR(35) NOT NULL,
    quote        TEXT NOT NULL,
    author_role  VARCHAR(500) NOT NULL DEFAULT '',
    organisation VARCHAR(500) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_testimonial (id) ON DELETE CASCADE
) ENGINE=InnoDB;
