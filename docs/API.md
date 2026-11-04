# API WasteNot

Base path `/v1`. JSON UTF-8, snake_case, UUID, timestamp RFC3339 UTC. Semua endpoint di bawah membutuhkan `Authorization: Bearer <access_token>`, kecuali register/login/refresh dan `/health/*`. Respons pribadi memakai `Cache-Control: no-store`; setiap request memperoleh UUID server pada `X-Request-ID`.

| Method | Path | Hasil |
|---|---|---|
| POST | `/v1/auth/register` | 201 user dan pasangan token |
| POST | `/v1/auth/login` | 200 user dan pasangan token |
| POST | `/v1/auth/refresh` | 200 pasangan token baru |
| POST | `/v1/auth/logout` | 204 cabut sesi sekarang |
| GET | `/v1/me` | Profil dan progres dari catatan aktif |
| GET | `/v1/metadata` | Enum, batas form, capability |
| GET/POST | `/v1/waste-logs` | Daftar / buat catatan |
| GET/PATCH/DELETE | `/v1/waste-logs/{id}` | Detail / edit / soft-delete milik sendiri |
| GET | `/v1/dashboard` | Hari ini menurut zona akun, tiga catatan terbaru, progres |
| GET | `/v1/statistics` | Ringkasan, metode, kategori dan seluruh tanggal dalam rentang |
| GET | `/v1/waste-guide` | Panduan PUBLISHED; `q`, `category`, `limit`, `cursor` |
| GET | `/v1/waste-guide/{id}` | Detail panduan terbitan |
| GET | `/v1/ai/objects` | Katalog keputusan + capability model aktif |
| POST | `/v1/ai/detections` | Multipart gambar ephemeral + `client_request_id` |
| POST | `/v1/ai/recommendations` | Keputusan Python + sesi rekomendasi milik pengguna |
| DELETE | `/v1/ai/detections/{id}` | 204 batalkan sesi dan rekomendasi terkait |
| GET | `/health/live` | Proses HTTP hidup |
| GET | `/health/ready` | DB dan migrasi inti tersedia |

Sukses berbentuk `{"data": ...}`; daftar memiliki `page: {"next_cursor":null,"has_more":false}`. Error berbentuk `{"error":{"code":"VALIDATION_ERROR","message":"...","fields":{...},"retryable":false},"request_id":"...","api_version":"1.0.0"}`. Field tambahan seperti request_id tidak mengubah `DataEnvelope` Android.

## Akun dan sesi

Register menerima `name`, `email`, `password`, `timezone`. Nama di-trim 2–60 karakter, email dinormalisasi huruf kecil dan maksimal 254 byte, password 12–128 karakter tanpa trim, zona IANA valid dan dikunci setelah registrasi. Password memakai Argon2id (19 MiB, dua iterasi, paralelisme satu) melalui library; parameter/salt tersimpan pada hash. Lihat [acuan OWASP](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).

Access token HS256 berlaku 15 menit; sesi refresh maksimal tujuh hari sejak login/register. Claim issuer, audience, expiry, issued-at, sub, sid, dan generation diverifikasi. Setiap request memeriksa sesi DB, sehingga logout dan rotasi langsung menolak access token lama. Refresh memakai token opaque acak 32 byte; server menyimpan SHA-256 saja. Rotasi atomik, replay token lama mencabut sesi tersebut. Android harus menserialisasi refresh dan tidak mengulangi request refresh yang hasilnya tidak diketahui. Logout berulang dengan token sah yang belum expired tetap 204.

Auth dibatasi 20 request per IP dan per identitas dalam jendela 15 menit. Batas request akun 300/menit. Forwarded headers tidak dipercaya; lihat konfigurasi ingress dalam OPERATIONS.md.

## Pencatatan

Contoh format create, dengan tanggal yang harus disesuaikan ke hari ini dalam timezone akun:

```json
{
  "client_request_id": "a55d801c-e1e0-4b8e-966c-cac4ac9324af",
  "category": "PLASTIC",
  "method": "RECYCLE",
  "quantity_kind": "WEIGHT",
  "weight_g": 250,
  "item_count": null,
  "activity_date": "2026-11-04",
  "notes": ""
}
```

WEIGHT wajib 1–100000 gram dan item_count null. COUNT wajib 1–10000 benda dan weight_g null. Tanggal create hari ini sampai 30 hari sebelumnya menurut zona akun. Catatan di-trim maksimal 500 karakter. Identitas pemilik, timestamp dan XP tidak diterima dari client. `photo_id` hanya null sampai lampiran permanen diimplementasikan.

Buat satu client_request_id per draf, lalu pertahankan ID dan payload saat retry timeout. Create baru 201; replay kanonis identik 200 dengan log yang sama; payload berubah 409 IDEMPOTENCY_CONFLICT; replay log terhapus 410 LOG_DELETED. Identitas create tetap disimpan setelah soft-delete. Replay diperiksa sebelum jendela tanggal dan TTL provenance, agar request yang sebelumnya sudah commit dapat ditemukan lagi.

PATCH hanya menerima category, method, quantity_kind, weight_g, item_count, activity_date, notes dan version. Field tidak dikirim tetap; notes null menjadi kosong. Setelah merge, validasi XOR unit tetap berlaku. Tanggal lama yang tidak berubah boleh berada di luar 30 hari. Version tidak cocok menghasilkan 409 VERSION_CONFLICT. DELETE wajib `If-Match: "2"`; missing header 428, version berbeda 409, sukses/replay delete milik sendiri 204. Objek asing/hilang/terhapus pada GET dan PATCH selalu 404.

Daftar menggunakan filter AND category/method/from/to, tanggal inklusif, limit 1–100 default 20. Urutan `activity_date DESC,id DESC`. Cursor bertanda tangan HMAC terikat pengguna dan filter; perubahan filter membutuhkan halaman pertama. Cursor bukan otorisasi dan tidak memuat token. Perubahan data selama pagination memerlukan deduplikasi ID di Android.

## Statistik

Default tujuh hari sampai hari ini; from/to inklusif maksimal 366 hari dan tanpa tanggal mendatang. Dashboard memakai timezone akun dan snapshot transaksi konsisten. Hari/kategori/metode tanpa data dikirim nol. Reduce terpisah dari sampah ditangani. Berat dan jumlah benda tidak dijumlahkan. Recycling rate = jumlah aktivitas Recycle / aktivitas Reuse+Recycle+Trash; denominator nol menghasilkan null. XP = catatan aktif × 10, level = floor(XP/100)+1. Streak memakai hari berbeda, tetap mempertahankan streak kemarin jika hari ini belum mencatat.

Panduan tidak diisi contoh desain. Impor konten nyata melalui `go run ./cmd/guides reviewed-guides.json`; importer mensyaratkan UUID, enum, langkah, sumber HTTPS, reviewer dan tanggal review. Hanya status PUBLISHED yang dapat dibaca. Format entitas lengkap terdapat pada `internal/database/guides.go`; importer memperbarui berdasarkan id secara atomik dan tidak membuat akun admin publik.

Error stabil utama: 400 INVALID_REQUEST, 401 UNAUTHENTICATED/SESSION_EXPIRED, 404 NOT_FOUND, 409 EMAIL_EXISTS/VERSION_CONFLICT/IDEMPOTENCY_CONFLICT, 410 LOG_DELETED/AI_SESSION_EXPIRED, 413 FILE_TOO_LARGE/REQUEST_TOO_LARGE, 415 UNSUPPORTED_MEDIA_TYPE, 422 VALIDATION_ERROR, 428 PRECONDITION_REQUIRED, 429 RATE_LIMITED, 503 UNAVAILABLE/AI_DISABLED/AI_UNAVAILABLE/INVALID_AI_RESPONSE, 504 AI_TIMEOUT. Hormati Retry-After; jangan membuat retry loop atau mengganti UUID otomatis.
