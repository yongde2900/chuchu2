-- properties 資料表：包租代管服務的物件主檔。
-- 唯一索引 properties_address_key 用於辨識重複建檔（同地址不可重複建檔），
-- 索引名稱刻意保持穩定，供上層以 SQLSTATE 23505 判斷衝突來源。
CREATE TABLE properties (
    id              UUID PRIMARY KEY,
    city            TEXT NOT NULL,
    district        TEXT NOT NULL,
    street_address  TEXT NOT NULL,
    floor           TEXT NOT NULL,
    room_no         TEXT NOT NULL,
    layout          TEXT NOT NULL,
    area_ping       NUMERIC(10,2) NOT NULL,
    monthly_rent    NUMERIC(12,2) NOT NULL,
    management_fee  NUMERIC(12,2) NOT NULL DEFAULT 0,
    deposit_months  INT NOT NULL,
    rental_mode     TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'VACANT',
    landlord_name   TEXT NOT NULL,
    landlord_phone  TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    CONSTRAINT properties_rental_mode_check CHECK (rental_mode IN ('MASTER_LEASE', 'MANAGED')),
    CONSTRAINT properties_status_check CHECK (status IN ('VACANT', 'OCCUPIED', 'RENOVATING', 'DELISTED')),
    CONSTRAINT properties_layout_check CHECK (layout IN ('WHOLE_UNIT', 'INDEPENDENT_SUITE', 'SHARED_SUITE', 'SINGLE_ROOM'))
);

CREATE UNIQUE INDEX properties_address_key ON properties (city, district, street_address, floor, room_no);
