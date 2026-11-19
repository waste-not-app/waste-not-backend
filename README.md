# WasteNot Backend

REST API Go untuk aplikasi [WasteNot Android](https://github.com/waste-not-app/waste-not-android). Layanan ini mengelola akun dan sesi, catatan sampah, dashboard, statistik, XP/level/streak, achievement, foto privat, panduan pengelolaan sampah, serta adapter ke worker AI ([waste-not-ai](https://github.com/waste-not-app/waste-not-ai)) untuk deteksi objek dan rekomendasi.

| Komponen | Teknologi |
|---|---|
| Bahasa | Go 1.27 |
| HTTP | Fiber v3 |
| Database | PostgreSQL 18, pgx v5, query type-safe via sqlc |
| Auth | JWT HS256 (access 15 menit) + refresh token rotasi dengan deteksi reuse, Argon2id |
| Dokumentasi API | OpenAPI (`contracts/`), Swagger opsional |
| Container | Distroless static, nonroot, read-only |

## Daftar isi

- [Mulai cepat dengan Docker](#mulai-cepat-dengan-docker)
- [Menjalankan tanpa Docker](#menjalankan-tanpa-docker)
- [Konfigurasi](#konfigurasi)
- [Perintah Make](#perintah-make)
- [Kualitas dan keamanan](#kualitas-dan-keamanan)
- [Struktur proyek](#struktur-proyek)
- [Database dan migrasi](#database-dan-migrasi)
- [Deployment](#deployment)
- [Dokumentasi lanjutan](#dokumentasi-lanjutan)

## Mulai cepat dengan Docker

Prasyarat: Docker dengan Compose v2, `make`, dan `openssl`.

```sh
make env    # membuat .env dengan secret lokal acak
make up     # db + migrate + import guides + api
curl http://127.0.0.1:8080/health/ready
make logs
make down
```

Stack lokal (`compose.dev.yaml`) membuka API di `127.0.0.1:8080`, PostgreSQL di `127.0.0.1:5432`, mengaktifkan lampiran foto pada volume Docker, dan Swagger UI di `http://127.0.0.1:8080/swagger/index.html`. AI nonaktif secara default; untuk menyambungkannya, jalankan worker dari repo AI di host lalu set `AI_ENABLED=true` beserta token di `.env`.

`compose.yaml` adalah konfigurasi staging (host network, bind mount foto) yang dipakai workflow CD, bukan untuk pengembangan lokal.

## Menjalankan tanpa Docker

Prasyarat: Go 1.27.4 dan PostgreSQL 18.

```sh
make env                 # lalu sesuaikan DATABASE_URL dengan PostgreSQL lokal
make migrate
make guides
make run
```

Binary tidak membaca `.env` sendiri; target `make` memuatnya. `make build` menghasilkan `bin/api`, `bin/migrate`, dan `bin/guides`. Binary API juga menyediakan `api healthcheck` untuk probe container.

## Konfigurasi

Semua konfigurasi lewat environment variable. Contoh lengkap ada di [`.env.example`](.env.example).

| Variabel | Wajib | Default | Keterangan |
|---|---|---|---|
| `DATABASE_URL` | ya | | URL PostgreSQL |
| `JWT_SECRET_BASE64` | ya | | Minimal 32 byte acak, base64 (`openssl rand -base64 32`) |
| `HTTP_ADDR` | | `127.0.0.1:8080` | Alamat listen |
| `WASTENOT_RELEASE` | | | Dikirim di header `X-WasteNot-Release` pada `/health/ready` |
| `AI_ENABLED` | | `false` | Mengaktifkan deteksi AI |
| `AI_RECOMMENDATION_ENABLED` | | ikut `AI_ENABLED` | Mengaktifkan rekomendasi AI |
| `AI_ALLOW_PROTOTYPE` | | `false` | Menerima hasil fixture/draf (hanya untuk pengujian) |
| `AI_DETECTION_URL`, `AI_RECOMMENDATION_URL` | bila AI aktif | | Origin worker privat |
| `AI_SERVICE_TOKEN`, `AI_RECOMMENDATION_TOKEN` | bila AI aktif | | Berbeda satu sama lain, minimal 24 karakter |
| `PHOTO_STORAGE_DIR` | | | Direktori privat untuk lampiran foto; kosong berarti fitur foto nonaktif |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | untuk reset password | port `587` | SMTP dengan STARTTLS atau TLS implisit (465) |
| `SWAGGER_ENABLED` | | `false` | Swagger UI di `/swagger/index.html`; jangan aktifkan di server publik |

## Perintah Make

Jalankan `make help` untuk daftar lengkap. Yang paling sering dipakai:

| Target | Fungsi |
|---|---|
| `make up` / `make down` / `make logs` | Stack Docker lokal |
| `make run` | Menjalankan API dari source |
| `make fmt` | Format kode (gofmt + goimports) |
| `make lint` | golangci-lint, termasuk gosec, errorlint, staticcheck |
| `make vuln` | govulncheck untuk dependency dan call graph |
| `make sec` | `lint` + `vuln` |
| `make test` | Unit test (integration test dilewati tanpa database) |
| `make test-integration` | Semua test terhadap PostgreSQL sementara di Docker |
| `make cover` | Laporan coverage |
| `make check` | Gate lokal: format, vet, lint, test |
| `make ci` | Gate CI: format, vet, lint, vuln, race test, build |
| `make sqlc` / `make swag` | Generate ulang query dan dokumentasi Swagger |
| `make docker-build` | Build image produksi |

Versi tool dipin di Makefile dan dijalankan lewat `go run`, jadi tidak perlu instalasi global.

## Kualitas dan keamanan

- **CI** (`.github/workflows/ci.yml`): verifikasi modul, cek kontrak AI, test deploy Python, `make ci` dengan PostgreSQL 18 dan race detector, lalu build image.
- **CD** (`.github/workflows/cd.yml`): deploy staging hanya setelah CI di `main` sukses.
- **Dependabot** memperbarui modul Go, GitHub Actions, dan base image setiap minggu.
- **Runtime**: image distroless nonroot dengan filesystem read-only, `cap_drop: ALL`, dan healthcheck. Rate limit dan idempotensi disimpan di PostgreSQL sehingga berlaku antar instance. Slot hashing, decode foto, dan scan AI dibatasi per proses.
- **Kebijakan data**: tidak ada akun demo, panduan palsu, atau hasil AI pengganti. Hasil fixture AI ditolak kecuali `AI_ALLOW_PROTOTYPE=true`.

## Struktur proyek

```
cmd/
  api/        entrypoint HTTP + subcommand healthcheck
  migrate/    runner migrasi
  guides/     import panduan yang sudah ditinjau
internal/
  server/     komposisi dependency, routing, worker latar, dan test end-to-end
  modules/    fitur: auth, logs, analytics, achievements, photos, guides, ai, system
              tiap modul: handler (HTTP) -> service (aturan) -> repository (SQL dan transaksi)
  domain/     tipe dan aturan bisnis bersama tanpa dependency HTTP atau database
  database/   koneksi, migrasi tertanam, import panduan; db/ berisi hasil sqlc
  aiclient/   klien worker AI dan validasi kontrak
  security/   JWT, hashing password Argon2id, digest
  middleware/ autentikasi dan request ID
  ratelimit/  kuota berbasis Postgres dan slot konkurensi
  cursor/     cursor paginasi bertanda tangan
  response/   envelope JSON dan pemetaan error
  config/     konfigurasi dari environment
contracts/    kontrak OpenAPI core, AI, dan fixture Android
sql/          migrasi dan query sumber sqlc
deploy/       skrip blue-green, nginx, systemd, hardening DB
docs/         dokumentasi lanjutan dan Swagger hasil generate
guides/       konten panduan yang sudah ditinjau
```

## Database dan migrasi

- Migrasi tidak dijalankan otomatis saat API start. Jalankan `cmd/migrate` sebagai langkah deploy terpisah (Compose melakukannya lewat service `migrate`).
- `/health/ready` baru melaporkan siap setelah migrasi terbaru yang tertanam di binary sudah diterapkan.
- Skema ada di dua tempat: `internal/database/*.sql` (dibaca runner, checksum tercatat di database produksi, jangan diubah) dan `sql/migrations/*.up.sql` (sumber sqlc). Setiap perubahan skema ditambahkan sebagai file baru di keduanya. `TestSchemaCopiesStayInSync` memastikan hasilnya identik.
- Query SQL ditulis di `sql/queries/*.sql` lalu di-generate ke `internal/database/db` dengan `make sqlc`. Jangan edit hasil generate secara manual.

## Deployment

Staging berjalan di VM cp-bcc melalui GitHub Actions dengan runner self-hosted. Nginx harus meneruskan `X-Real-IP` (lihat `deploy/nginx-location.conf`) agar rate limit login dihitung per klien. Rincian blue-green, systemd, backup, dan rollback ada di [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) dan [docs/OPERATIONS.md](docs/OPERATIONS.md).

Base URL staging: <https://pred-autoscaler.bccdev.id/wastenot/api/v1/>

## Dokumentasi lanjutan

- [Panduan integrasi Android dan AI](docs/INTEGRATION.md)
- [Panduan API](docs/API.md): envelope, endpoint, retry, dan konflik
- [OpenAPI core](contracts/core.openapi.json) dan [OpenAPI AI publik](contracts/ai/public.openapi.json)
- [Integrasi Android](docs/ANDROID.md)
- [Integrasi AI](docs/AI.md)
- [Kurasi panduan](guides/README.md)
- [Operasional](docs/OPERATIONS.md)
- [Bukti pengujian](docs/VERIFICATION.md)
- [Panduan kontribusi](CONTRIBUTING.md)
