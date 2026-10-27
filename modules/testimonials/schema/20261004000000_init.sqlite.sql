-- Testimonials are a collection (see package content).
CREATE TABLE wt_testimonial (
    id         TEXT PRIMARY KEY,
    instance   TEXT NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    deleted_at TIMESTAMP,
    deleted_by TEXT,
    photo_id   TEXT,
    author     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX wt_testimonial_instance ON wt_testimonial (instance, position);

CREATE TABLE wt_testimonial_translation (
    item_id      TEXT NOT NULL REFERENCES wt_testimonial (id) ON DELETE CASCADE,
    locale       TEXT NOT NULL,
    quote        TEXT NOT NULL DEFAULT '',
    author_role  TEXT NOT NULL DEFAULT '',
    organisation TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
