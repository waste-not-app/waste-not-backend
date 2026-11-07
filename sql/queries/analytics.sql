-- name: AggregateWasteLogsByDateRange :many
SELECT activity_date::text AS activity_date, category, method, count(*)::bigint AS activity_count, COALESCE(sum(weight_g), 0)::bigint AS total_weight_g, COALESCE(sum(item_count), 0)::bigint AS total_item_count
FROM waste_logs
WHERE user_id = sqlc.arg(user_id)
  AND deleted_at IS NULL
  AND activity_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
GROUP BY activity_date, category, method;

-- name: ListRecentActiveLogs :many
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, version, created_at, updated_at
FROM waste_logs
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY activity_date DESC, id DESC
LIMIT 3;
