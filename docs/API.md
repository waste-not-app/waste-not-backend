# API WasteNot

Base path `/v1`. JSON UTF-8, snake_case, UUID, timestamp RFC3339 UTC. Semua endpoint di bawah membutuhkan `Authorization: Bearer <access_token>`, kecuali register/login/refresh/password-reset/password-reset/confirm dan `/health/*`. Health readiness menambahkan header `X-WasteNot-Release` ketika identitas rilis dikonfigurasi; body readiness tetap `{"ready":true}` dan tidak membuktikan worker/Gemini siap. Respons pribadi memakai `Cache-Control: no-store`; setiap request memperoleh UUID server pada `X-Request-ID`.

| Method | Path | Hasil |
|---|---|---|
| POST | `/v1/auth/register` | 201 user dan pasangan token |
| POST | `/v1/auth/login` | 200 user dan pasangan token |
| POST | `/v1/auth/refresh` | 200 pasangan token baru |
| POST | `/v1/auth/logout` | 204 cabut sesi sekarang |
| POST | `/v1/auth/password-reset` | 202 permintaan pemulihan diterima secara kondisional |
| POST | `/v1/auth/password-reset/confirm` | 204 ganti password dan cabut seluruh sesi |
| DELETE | `/v1/me` | 204 hapus akun, data milik akun dan foto privat |
| GET | `/v1/me` | Profil dan progres dari catatan aktif |
| GET | `/v1/metadata` | Enum, batas form, capability |
| GET/POST | `/v1/waste-logs` | Daftar / buat catatan |
| GET/PATCH/DELETE | `/v1/waste-logs/{id}` | Detail / edit / soft-delete milik sendiri |
| GET | `/v1/dashboard` | Hari ini menurut zona akun, tiga catatan terbaru, progres |
| GET | `/v1/statistics` | Ringkasan, metode, kategori dan seluruh tanggal dalam rentang |
| GET | `/v1/waste-guide` | Panduan PUBLISHED; `q`, `category`, `limit`, `cursor` |
| GET | `/v1/waste-guide/{id}` | Detail panduan terbitan |
| GET | `/v1/achievements` | Daftar achievement dan progres turunan |
| POST | `/v1/photos` | Upload multipart foto lampiran privat (`STAGED`) |
| GET | `/v1/photos/{id}` | Unduh foto lampiran milik sendiri |
| DELETE | `/v1/photos/{id}` | Hapus foto `STAGED` yang belum dipakai |
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

`POST /v1/auth/password-reset` menerima `{"email":"..."}`. Email dinormalisasi seperti login. Respons 202 selalu sama untuk email terdaftar maupun tidak: `{"data":{"message":"Jika email terdaftar, permintaan pengaturan ulang kata sandi akan diproses. Periksa email untuk kode pemulihan."},"request_id":"...","api_version":"1.0.0"}`. Ini berarti permintaan diterima, bukan bukti email sudah terkirim. Server hanya mengantrekan email untuk akun yang ada. Konfigurasi SMTP yang belum lengkap, atau kegagalan probe koneksi TLS/autentikasi SMTP yang dilakukan sebelum pencarian akun, menghasilkan 503 `UNAVAILABLE` untuk kedua kasus.

Email berisi kode opaque acak 32 byte (base64url 43 karakter), untuk ditempel di aplikasi, bukan URL. Verifier di `password_reset_tokens` hanya SHA-256; payload transport di outbox terpisah dienkripsi AES-256-GCM dengan nonce acak dan kunci turunan `JWT_SECRET_BASE64`, terikat pada alamat email. Worker API memproses outbox durable, memakai timeout per pengiriman dan maksimal lima percobaan dengan jeda 30, 60, 120, 240 detik. Payload dihapus setelah pengiriman, konfirmasi, penggantian permintaan atau penghapusan akun; pemeliharaan juga membuang payload kedaluwarsa. Kode tidak masuk HTTP, URL atau log. Rotasi secret membuat payload lama tidak dapat dibuka dan pengguna perlu meminta kode baru.

Worker mengklaim versi outbox dalam transaksi singkat lalu menutup transaksi sebelum SMTP. Lease 30 detik memulihkan pekerjaan jika proses berhenti; pengiriman memakai timeout yang lebih pendek. Penyelesaian sukses maupun penjadwalan ulang hanya berlaku jika versi pekerjaan masih sama, sehingga pengiriman lama tidak dapat menghapus atau menunda permintaan pengganti. Koneksi SMTP yang lambat tidak memegang lock akun dan tidak memblokir request reset berikutnya.

`POST /v1/auth/password-reset/confirm` menerima `{"token":"...","password":"..."}` dan menghasilkan 204 tanpa body. Password baru mengikuti batas register. Kode berlaku 15 menit sejak permintaan; permintaan baru mengganti kode sebelumnya. Kode salah, kedaluwarsa, diganti, atau sudah dipakai menghasilkan 422 `INVALID_RESET_TOKEN`, `fields.token`. Penggantian password, konsumsi kode/outbox dan penghapusan seluruh sesi berlangsung dalam satu transaksi. Login yang sudah memverifikasi password lama tidak dapat membuat sesi setelah penggantian commit. Request reset dibatasi per IP/email; konfirmasi dibatasi per IP/hash token dalam jendela auth yang sama.

`DELETE /v1/me` menerima `{"password":"kata sandi saat ini"}`. Password salah menghasilkan 403 `WRONG_PASSWORD` dengan `fields.password`, bukan 401; sesi tetap berlaku. Setelah verifikasi, transaksi menghapus hanya pemilik terautentikasi: user, seluruh sesi/refresh token, log dan metadata AI, operasi AI, achievement pengguna, kode reset/outbox dan baris foto. Lock akun menserialisasi unggahan file dengan penghapusan; penyelesaian operasi AI yang telanjur berjalan tidak membuat ulang data yang sudah dihapus.

Sebelum menyentuh filesystem, unggahan meng-commit intent durable dalam `photo_cleanup_jobs`. Transaksi berikutnya mengunci akun dan intent selama penulisan, lalu meng-commit metadata foto sekaligus menghapus intent. Jika proses berhenti sebelum commit kedua, intent tetap ada dan melacak file final maupun file sementara deterministik `<object_key>.upload`. Cleanup melewati intent yang sedang dikunci penulis; intent yang ditinggalkan dapat dibersihkan setelah dua menit. Penghapusan akun juga menguras intent pemilik tanpa menunggu masa tunggu itu, dan tidak mengonfirmasi 204 jika intent atau pekerjaan cleanup pemilik masih tersisa.

Trigger foto membuat pekerjaan penghapusan file durable, termasuk foto yang sudah berstatus `DELETED`. Endpoint menunggu antrean pemilik selesai sebelum 204. Jika cascade sudah commit tetapi storage gagal, responsnya 503 `ACCOUNT_DELETION_PENDING`: akun dan sesi sudah hilang, file tetap di antrean, worker pemeliharaan mencoba lagi. Android harus membersihkan cache/antrean lokal milik akun dan keluar pada kode khusus ini, sambil menjelaskan bahwa penghapusan foto server belum selesai; jangan menyatakan purge berhasil atau mengulang dengan sesi lama. Kegagalan sebelum commit memakai error biasa dan tidak mengonfirmasi penghapusan. Counter rate-limit hash dan salinan backup tidak dipurge oleh endpoint ini; pemeliharaan counter mengikuti retensi operasionalnya.

Kegagalan penghapusan satu objek menaikkan jumlah percobaan dan menunda pekerjaan secara durable, mulai 30 detik dengan jeda berlipat hingga 32 menit. Worker tetap memproses pekerjaan pemilik lain yang sudah siap. Antrean yang ditunda atau sedang dikunci worker lain tetap dihitung sebagai cleanup pemilik yang belum selesai, sehingga tidak berubah menjadi konfirmasi purge palsu.

Konfigurasi mail: `SMTP_HOST`, `SMTP_PORT` (default 587), `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`. Port 465 memakai TLS langsung; port lain wajib STARTTLS. Sertifikat dan nama host selalu diverifikasi, minimal TLS 1.2; tidak ada mode plaintext atau bypass sertifikat. Pengiriman baru memerlukan kredensial dan pengirim SMTP runtime yang benar.

Migrasi baru: `003_account_recovery.sql` pada runner embedded dan `000003_account_recovery.up.sql` untuk sqlc. Jalankan migrasi dengan `go run ./cmd/migrate` sebelum menyalakan API baru; migrasi bersifat aditif dan tidak mengubah checksum migrasi sebelumnya. `sqlc generate` memperbarui query/model dari sumber SQL. Pemilik proses wajib memanggil `Server.Close()` agar worker mail dan cleanup berhenti sebelum pool database ditutup.

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

WEIGHT wajib 1–100000 gram dan item_count null. COUNT wajib 1–10000 benda dan weight_g null. Tanggal create hari ini sampai 30 hari sebelumnya menurut zona akun. Catatan di-trim maksimal 500 karakter. Identitas pemilik, timestamp dan XP tidak diterima dari client. `photo_id` menerima UUID foto `STAGED` milik pengguna jika `capabilities.photos=true` (atau `null`).

Buat satu client_request_id per draf, lalu pertahankan ID dan payload saat retry timeout. Create baru 201; replay kanonis identik 200 dengan log yang sama; payload berubah 409 IDEMPOTENCY_CONFLICT; replay log terhapus 410 LOG_DELETED. Identitas create tetap disimpan setelah soft-delete. Replay diperiksa sebelum jendela tanggal dan TTL provenance, agar request yang sebelumnya sudah commit dapat ditemukan lagi.

PATCH hanya menerima category, method, quantity_kind, weight_g, item_count, activity_date, notes, photo_id dan version. Field tidak dikirim tetap; notes null menjadi kosong; photo_id null melepas lampiran lama dan menandainya terhapus. Setelah merge, validasi XOR unit tetap berlaku. Tanggal lama yang tidak berubah boleh berada di luar 30 hari. Version tidak cocok menghasilkan 409 VERSION_CONFLICT. DELETE wajib `If-Match: "2"`; missing header 428, version berbeda 409, sukses/replay delete milik sendiri 204 (dan langsung mencabut akses foto terkait). Objek asing/hilang/terhapus pada GET dan PATCH selalu 404.

Daftar menggunakan filter AND category/method/from/to, tanggal inklusif, limit 1–100 default 20. Urutan `activity_date DESC,id DESC`. Cursor bertanda tangan HMAC terikat pengguna dan filter; perubahan filter membutuhkan halaman pertama. Cursor bukan otorisasi dan tidak memuat token. Perubahan data selama pagination memerlukan deduplikasi ID di Android.

## Statistik

Default tujuh hari sampai hari ini; from/to inklusif maksimal 366 hari dan tanpa tanggal mendatang. Dashboard memakai timezone akun dan snapshot transaksi konsisten. Hari/kategori/metode tanpa data dikirim nol. Reduce terpisah dari sampah ditangani. Berat dan jumlah benda tidak dijumlahkan. Recycling rate = jumlah aktivitas Recycle / aktivitas Reuse+Recycle+Trash; denominator nol menghasilkan null. XP = catatan aktif × 10, level = floor(XP/100)+1. Streak memakai hari berbeda, tetap mempertahankan streak kemarin jika hari ini belum mencatat.

Panduan tidak diisi contoh desain. Draf kurasi awal 16 artikel tersedia di `guides/core-guides-v1.json`; setelah ditinjau penanggung jawab konten dan status diubah ke `PUBLISHED`, impor via `go run ./cmd/guides guides/core-guides-v1.json`. Importer mensyaratkan UUID, enum, langkah, sumber HTTPS, reviewer dan tanggal review. Hanya status `PUBLISHED` yang dapat dibaca pada `/v1/waste-guide`.

Error stabil utama: 400 INVALID_REQUEST, 401 UNAUTHENTICATED/SESSION_EXPIRED, 403 WRONG_PASSWORD, 404 NOT_FOUND, 409 EMAIL_EXISTS/VERSION_CONFLICT/IDEMPOTENCY_CONFLICT/PHOTO_ALREADY_LINKED/PHOTO_IN_USE, 410 LOG_DELETED/AI_SESSION_EXPIRED, 413 FILE_TOO_LARGE/REQUEST_TOO_LARGE, 415 UNSUPPORTED_MEDIA_TYPE, 422 VALIDATION_ERROR/INVALID_RESET_TOKEN/INVALID_IMAGE/IMAGE_DIMENSIONS_EXCEEDED, 428 PRECONDITION_REQUIRED, 429 RATE_LIMITED, 503 UNAVAILABLE/ACCOUNT_DELETION_PENDING/AI_DISABLED/AI_UNAVAILABLE/INVALID_AI_RESPONSE, 504 AI_TIMEOUT. Hormati Retry-After; jangan membuat retry loop atau mengganti UUID otomatis.
