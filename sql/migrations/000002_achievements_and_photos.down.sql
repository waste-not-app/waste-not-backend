DROP INDEX IF EXISTS waste_logs_active_photo;
ALTER TABLE waste_logs DROP COLUMN IF EXISTS photo_id;
DROP INDEX IF EXISTS photos_user_status;
DROP TABLE IF EXISTS photos;
DROP TABLE IF EXISTS user_achievements;
DROP TABLE IF EXISTS achievements;
