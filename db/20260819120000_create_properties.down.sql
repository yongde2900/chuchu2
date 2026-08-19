-- 回滾 20260819120000_create_properties：乾淨移除索引與資料表，
-- 讓 up -> down -> up 可重複執行。
DROP INDEX IF EXISTS properties_address_key;
DROP TABLE IF EXISTS properties;
