CREATE TABLE line_users (
    line_user_id   TEXT PRIMARY KEY,
    status         TEXT NOT NULL,
    -- LINE 事件的毫秒 Unix 時間戳，唯一用途是比大小擋亂序重送，
    -- 刻意不用 TIMESTAMPTZ：轉型只會多一個轉換出錯的地方，沒有其他好處。
    last_event_at  BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    CONSTRAINT line_users_status_check CHECK (status IN ('FOLLOWING', 'BLOCKED'))
);
