CREATE TABLE wt_media_asset (
    id          VARCHAR(36) PRIMARY KEY,
    storage_key VARCHAR(120) NOT NULL UNIQUE,
    filename    VARCHAR(255) NOT NULL,
    mime_type   VARCHAR(100) NOT NULL,
    size_bytes  BIGINT NOT NULL,
    width       INTEGER NOT NULL DEFAULT 0,
    height      INTEGER NOT NULL DEFAULT 0,
    -- The point crops keep in view, as fractions of the upright width and
    -- height from the top left (see media.FocalPoint).
    focal_x     DOUBLE NOT NULL DEFAULT 0.5,
    focal_y     DOUBLE NOT NULL DEFAULT 0.5,
    checksum    VARCHAR(64) NOT NULL,
    created_at  DATETIME(6) NOT NULL,
    created_by  VARCHAR(36) NULL,
    updated_at  DATETIME(6) NOT NULL,
    updated_by  VARCHAR(36) NULL,
    deleted_at  DATETIME(6) NULL,
    deleted_by  VARCHAR(36) NULL
) ENGINE=InnoDB;

CREATE TABLE wt_media_translation (
    asset_id VARCHAR(36) NOT NULL,
    locale   VARCHAR(35) NOT NULL,
    title    VARCHAR(200) NOT NULL DEFAULT '',
    alt_text TEXT NOT NULL,
    PRIMARY KEY (asset_id, locale),
    FOREIGN KEY (asset_id) REFERENCES wt_media_asset (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Which content row references which asset. Content stores maintain it; the
-- foreign key stops deleting assets that are in use.
CREATE TABLE wt_media_usage (
    asset_id VARCHAR(36) NOT NULL,
    owner    VARCHAR(100) NOT NULL,
    owner_id VARCHAR(100) NOT NULL,
    PRIMARY KEY (asset_id, owner, owner_id),
    INDEX wt_media_usage_owner (owner, owner_id),
    FOREIGN KEY (asset_id) REFERENCES wt_media_asset (id)
) ENGINE=InnoDB;

-- Resized copies of images (see media.Variant), deleted with their asset.
CREATE TABLE wt_media_variant (
    storage_key VARCHAR(120) PRIMARY KEY,
    asset_id    VARCHAR(36) NOT NULL,
    created_at  DATETIME(6) NOT NULL,
    INDEX wt_media_variant_asset (asset_id),
    FOREIGN KEY (asset_id) REFERENCES wt_media_asset (id) ON DELETE CASCADE
) ENGINE=InnoDB;
