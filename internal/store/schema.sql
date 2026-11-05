CREATE TABLE IF NOT EXISTS accounts (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    site                  TEXT    NOT NULL DEFAULT '',
    remote_uid            TEXT    NOT NULL,
    name                  TEXT    NOT NULL DEFAULT '',
    avatar_url            TEXT    NOT NULL DEFAULT '',
    credential_enc        BLOB,
    proxy_enc             BLOB,
    credential_updated_at INTEGER NOT NULL DEFAULT 0,
    state                 TEXT    NOT NULL DEFAULT 'active',
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL,
    UNIQUE(site, remote_uid)
);

CREATE TABLE IF NOT EXISTS folders (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id        INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    remote_id         TEXT    NOT NULL,
    title             TEXT    NOT NULL,
    kind              TEXT    NOT NULL DEFAULT 'favorite',
    owner_id          TEXT    NOT NULL DEFAULT '',
    privacy           INTEGER NOT NULL DEFAULT 0,
    remote_count      INTEGER NOT NULL DEFAULT 0,
    state             TEXT    NOT NULL DEFAULT 'active',
    last_full_sync_at INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    UNIQUE(account_id, remote_id)
);

CREATE TABLE IF NOT EXISTS items (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    remote_id     TEXT    NOT NULL,
    kind          INTEGER NOT NULL DEFAULT 2,
    title         TEXT    NOT NULL,
    url           TEXT    NOT NULL,
    cover_url     TEXT    NOT NULL DEFAULT '',
    duration      INTEGER NOT NULL DEFAULT 0,
    author_id     TEXT    NOT NULL DEFAULT '',
    author_name   TEXT    NOT NULL DEFAULT '',
    summary       TEXT    NOT NULL DEFAULT '',
    published_at  INTEGER NOT NULL DEFAULT 0,
    attr          INTEGER NOT NULL DEFAULT 0,
    state         TEXT    NOT NULL DEFAULT 'active',
    first_seen_at INTEGER NOT NULL,
    last_seen_at  INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    UNIQUE(account_id, remote_id)
);

CREATE TABLE IF NOT EXISTS folder_items (
    folder_id     INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    item_id       INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    favorited_at  INTEGER NOT NULL DEFAULT 0,
    state         TEXT    NOT NULL DEFAULT 'active',
    missing_since INTEGER,
    missing_count INTEGER NOT NULL DEFAULT 0,
    removed_at    INTEGER,
    PRIMARY KEY (folder_id, item_id)
);

CREATE TABLE IF NOT EXISTS sync_runs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    trigger       TEXT    NOT NULL,
    scope         TEXT    NOT NULL DEFAULT 'all',
    state         TEXT    NOT NULL,
    started_at    INTEGER NOT NULL,
    finished_at   INTEGER,
    folders_total INTEGER NOT NULL DEFAULT 0,
    folders_done  INTEGER NOT NULL DEFAULT 0,
    items_seen    INTEGER NOT NULL DEFAULT 0,
    items_added   INTEGER NOT NULL DEFAULT 0,
    items_updated INTEGER NOT NULL DEFAULT 0,
    items_missing INTEGER NOT NULL DEFAULT 0,
    items_removed INTEGER NOT NULL DEFAULT 0,
    error_code    TEXT,
    error_message TEXT
);

CREATE TABLE IF NOT EXISTS item_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     INTEGER REFERENCES sync_runs(id) ON DELETE SET NULL,
    item_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    folder_id  INTEGER REFERENCES folders(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL,
    detail     TEXT,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_items_account_state ON items(account_id, state);
CREATE INDEX IF NOT EXISTS idx_items_account_author ON items(account_id, author_id);
CREATE INDEX IF NOT EXISTS idx_folder_items_state ON folder_items(folder_id, state);
CREATE INDEX IF NOT EXISTS idx_runs_account_started ON sync_runs(account_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_item ON item_events(item_id, created_at DESC);

CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS download_tasks (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    site           TEXT    NOT NULL DEFAULT '',
    page_url       TEXT    NOT NULL DEFAULT '',
    remote_id      TEXT    NOT NULL,
    part_id        INTEGER NOT NULL DEFAULT 0,
    video_title    TEXT    NOT NULL DEFAULT '',
    page_title     TEXT    NOT NULL DEFAULT '',
    page_no        INTEGER NOT NULL DEFAULT 1,
    page_count     INTEGER NOT NULL DEFAULT 1,
    duration       INTEGER NOT NULL DEFAULT 0,
    cover_url      TEXT    NOT NULL DEFAULT '',
    author_id      TEXT    NOT NULL DEFAULT '',
    author_name    TEXT    NOT NULL DEFAULT '',
    pub_time       INTEGER NOT NULL DEFAULT 0,
    fav_time       INTEGER NOT NULL DEFAULT 0,
    source_title   TEXT    NOT NULL DEFAULT '',
    tags           TEXT    NOT NULL DEFAULT '',
    summary        TEXT    NOT NULL DEFAULT '',
    quality        INTEGER NOT NULL DEFAULT 0,
    state          TEXT    NOT NULL DEFAULT 'pending',
    progress       REAL    NOT NULL DEFAULT 0,
    total_bytes    INTEGER NOT NULL DEFAULT 0,
    downloaded     INTEGER NOT NULL DEFAULT 0,
    actual_quality INTEGER NOT NULL DEFAULT 0,
    codec          TEXT    NOT NULL DEFAULT '',
    file_path      TEXT    NOT NULL DEFAULT '',
    error          TEXT    NOT NULL DEFAULT '',
    attempts       INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    finished_at    INTEGER,
    UNIQUE(account_id, remote_id, part_id, quality)
);

CREATE INDEX IF NOT EXISTS idx_downloads_account_state ON download_tasks(account_id, state, id);
CREATE INDEX IF NOT EXISTS idx_downloads_remote_id ON download_tasks(remote_id);
