-- name: GetAchievementMetrics :one
SELECT
    count(*)::bigint AS active_log_count,
    count(*) FILTER (WHERE method = 'RECYCLE')::bigint AS recycle_log_count,
    COALESCE(array_agg(DISTINCT activity_date::text ORDER BY activity_date::text), ARRAY[]::text[])::text[] AS activity_dates
FROM waste_logs
WHERE user_id = $1 AND deleted_at IS NULL;

-- name: ListUserAchievements :many
SELECT
    a.id::text AS id,
    a.code,
    a.title,
    a.description,
    a.requirement_type,
    a.requirement_value,
    ua.unlocked_at
FROM achievements a
LEFT JOIN user_achievements ua
    ON ua.achievement_id = a.id AND ua.user_id = $1
ORDER BY a.sort_order ASC, a.code ASC;

-- name: UnlockUserAchievement :exec
INSERT INTO user_achievements (user_id, achievement_id)
VALUES ($1, $2)
ON CONFLICT (user_id, achievement_id) DO NOTHING;

-- name: RelockUserAchievement :exec
DELETE FROM user_achievements
WHERE user_id = $1 AND achievement_id = $2;
