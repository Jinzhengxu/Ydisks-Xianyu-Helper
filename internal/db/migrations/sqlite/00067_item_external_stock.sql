-- +goose Up
CREATE TABLE item_external_stock (
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    source_url TEXT NOT NULL DEFAULT '',
    http_method TEXT NOT NULL DEFAULT 'GET' CHECK (http_method = 'GET'),
    headers_ciphertext TEXT NOT NULL DEFAULT '',
    json_path TEXT NOT NULL DEFAULT 'available',
    timeout_seconds INTEGER NOT NULL DEFAULT 20,
    user_agent TEXT NOT NULL DEFAULT '',
    cron_expr TEXT NOT NULL DEFAULT '1 0 * * *',
    timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    interval_minutes INTEGER NOT NULL DEFAULT 30,
    dry_run INTEGER NOT NULL DEFAULT 0 CHECK (dry_run IN (0, 1)),
    sync_platform_stock INTEGER NOT NULL DEFAULT 0 CHECK (sync_platform_stock IN (0, 1)),
    stock_cap INTEGER NOT NULL DEFAULT 999999,
    last_checked_unix INTEGER NOT NULL DEFAULT 0,
    last_available INTEGER,
    last_action TEXT NOT NULL DEFAULT '',
    last_status TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    backoff_until_unix INTEGER NOT NULL DEFAULT 0,
    backoff_class TEXT NOT NULL DEFAULT '',
    updated_at_unix INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (cookie_id, item_id),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE item_external_stock;
