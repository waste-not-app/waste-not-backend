CREATE INDEX refresh_tokens_session ON refresh_tokens(session_id);
CREATE INDEX ai_operations_detection ON ai_operations(detection_id) WHERE detection_id IS NOT NULL;
