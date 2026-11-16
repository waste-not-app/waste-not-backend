DROP TRIGGER photos_cleanup_after_retirement ON photos;
DROP TRIGGER photos_cleanup_after_delete ON photos;
DROP FUNCTION queue_retired_photo();
DROP FUNCTION queue_deleted_photo();
DROP TABLE photo_cleanup_jobs;
DROP TABLE password_reset_mail;
DROP TABLE password_reset_tokens;
