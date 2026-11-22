-- +goose Up
CREATE TABLE item_external_stock (
    cookie_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(255) NOT NULL,
    enabled TINYINT NOT NULL DEFAULT 0,
    source_url VARCHAR(2048) NOT NULL DEFAULT '',
    http_method VARCHAR(8) NOT NULL DEFAULT 'GET',
    headers_ciphertext TEXT NOT NULL,
    json_path VARCHAR(128) NOT NULL DEFAULT 'available',
    timeout_seconds INT NOT NULL DEFAULT 20,
    user_agent VARCHAR(256) NOT NULL DEFAULT '',
    cron_expr VARCHAR(64) NOT NULL DEFAULT '1 0 * * *',
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    interval_minutes INT NOT NULL DEFAULT 30,
    dry_run TINYINT NOT NULL DEFAULT 0,
    sync_platform_stock TINYINT NOT NULL DEFAULT 0,
    stock_cap INT NOT NULL DEFAULT 999999,
    last_checked_unix BIGINT NOT NULL DEFAULT 0,
    last_available INT NULL,
    last_action VARCHAR(32) NOT NULL DEFAULT '',
    last_status VARCHAR(32) NOT NULL DEFAULT '',
    last_error VARCHAR(512) NOT NULL DEFAULT '',
    backoff_until_unix BIGINT NOT NULL DEFAULT 0,
    backoff_class VARCHAR(16) NOT NULL DEFAULT '',
    updated_at_unix BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (cookie_id, item_id),
    CONSTRAINT chk_item_external_stock_enabled CHECK (enabled IN (0, 1)),
    CONSTRAINT chk_item_external_stock_method CHECK (http_method = 'GET'),
    CONSTRAINT chk_item_external_stock_dry_run CHECK (dry_run IN (0, 1)),
    CONSTRAINT chk_item_external_stock_sync CHECK (sync_platform_stock IN (0, 1)),
    CONSTRAINT fk_item_external_stock_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE item_external_stock;
