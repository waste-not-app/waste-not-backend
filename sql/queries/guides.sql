-- name: GetPublishedGuideByID :one
SELECT id::text AS id, slug, name, category, keywords, recommended_method, summary, steps, cautions, source_url, reviewed_at::text AS reviewed_at, reviewer, publication_status
FROM waste_guides
WHERE id = $1 AND publication_status = 'PUBLISHED';

-- name: ListPublishedGuides :many
SELECT id::text AS id, slug, name, category, keywords, recommended_method, summary, steps, cautions, source_url, reviewed_at::text AS reviewed_at, reviewer, publication_status
FROM waste_guides
WHERE publication_status = 'PUBLISHED'
  AND (sqlc.arg(category)::text = '' OR category = sqlc.arg(category)::text)
  AND (sqlc.arg(query)::text = '' OR strpos(lower(name || ' ' || array_to_string(keywords, ' ')), lower(sqlc.arg(query)::text)) > 0)
  AND (sqlc.arg(name_pos)::text = '' OR (name, id) > (sqlc.arg(name_pos)::text, NULLIF(sqlc.arg(id_pos)::text, '')::uuid))
ORDER BY name, id
LIMIT sqlc.arg(page_limit)::int;
