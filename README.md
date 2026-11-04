# WasteNot Backend

Backend Go untuk aplikasi [WasteNot Android](https://github.com/waste-not-app/waste-not-android). API HTTP `/v1` menggunakan PostgreSQL, mengikuti kontrak Android dan memakai worker Python yang sudah tersedia untuk YOLO/Gemini.

Implementasi mencakup akun/sesi, CRUD catatan, pagination/filter, dashboard, statistik, XP/level/streak, pencarian panduan, dan adapter AI dengan ownership, idempotensi lintas proses, TTL serta provenance saat simpan. Tidak membuat akun demo, panduan terbitan palsu, atau hasil AI pengganti. AI memiliki feature gate deteksi/rekomendasi terpisah; mode fixture/draf memerlukan pengaktifan eksplisit untuk pengujian.

## Menjalankan lokal

Prasyarat: Go 1.27.3, PostgreSQL 18; atau Docker Compose untuk seluruh layanan inti.

```sh
cp .env.example .env
# Isi JWT_SECRET_BASE64 dengan hasil: openssl rand -base64 32
# Isi POSTGRES_PASSWORD dengan password lokal acak (huruf/angka).
docker compose up --build -d
curl http://127.0.0.1:8080/health/ready
```

Database memakai volume persisten. Migrasi dijalankan sebagai layanan satu kali sebelum API. Port hanya terbuka pada loopback; Compose tidak mengaktifkan AI. Jangan memakai contoh konfigurasi lokal sebagai deployment internet.

Untuk menjalankan tanpa Docker, isi `DATABASE_URL` dan `JWT_SECRET_BASE64` di `.env`. Muat hanya file `.env` milik sendiri:

```sh
set -a
. ./.env
set +a
make migrate
make run
```

`DATABASE_URL` pada contoh `.env` memakai password placeholder. Sesuaikan dengan PostgreSQL sendiri; URL-encode password bila mengandung karakter khusus. Binary tidak membaca `.env` otomatis. `make build` menghasilkan `bin/api`, `bin/migrate`, dan `bin/guides`.

## API dan integrasi

- [Panduan API](docs/API.md): envelope, endpoint, retry, konflik, dan contoh request.
- [OpenAPI core](contracts/core.openapi.json) dan [OpenAPI AI publik](contracts/ai/public.openapi.json).
- [Integrasi Android](docs/ANDROID.md): konfigurasi base URL dan adapter Android yang masih harus dihubungkan.
- [Integrasi AI](docs/AI.md): menjalankan worker, feature gate, quota dan batas pengujian.
- [Operasional](docs/OPERATIONS.md): migrasi, backup, TLS, secret, scaling dan pemulihan.
- [Bukti pengujian](docs/VERIFICATION.md).

Kontrak diturunkan dari Android commit [`c125859`](https://github.com/waste-not-app/waste-not-android/commit/c1258596cb71bc7b95dc9a4f140f55f759815c4e). Fixture Android dan kontrak AI asli disalin tanpa perubahan ke [`contracts/`](contracts/README.md). Worker Python tetap menjadi pemilik tunggal keputusan AI. Go tidak memanggil Gemini langsung dan tidak memuat model YOLO.

## Pengujian

```sh
make test
# Database khusus pengujian; setiap suite membuat schema terisolasi dan menghapusnya.
export TEST_DATABASE_URL='postgres://postgres:password@127.0.0.1:5432/wastenot_test?sslmode=disable'
make check
make vuln
```

Tanpa `TEST_DATABASE_URL`, integration test dilewati dan tidak membuktikan alur database. CI mewajibkan PostgreSQL, race detector, vet, build binary, pemeriksaan dependensi dan build container. Tidak ada migrasi otomatis pada startup API; jalankan migrasi sebagai langkah deployment terpisah.

## Keputusan implementasi

Satu layanan Go, router `net/http`, SQL terparameterisasi melalui pgx, pool maksimal 10 koneksi, indeks riwayat aktif, pagination berbasis posisi, dan agregasi SQL. Tidak ada ORM, Redis atau antrean tambahan untuk kebutuhan inti. Dua slot decode gambar dan empat slot hashing membatasi pekerjaan berat per proses. Rate limit dan idempotensi memakai PostgreSQL sehingga berlaku antar instance.

Panduan kosong sampai konten yang benar-benar ditinjau diimpor. Achievement, pengingat, lampiran foto permanen dan titik daur ulang memiliki capability `false`. Upload AI bersifat sementara di memori dan berbeda dari lampiran foto. API sudah dideploy ke VM cp-bcc melalui GitHub Actions dan dapat dicapai di [base URL HTTPS](https://pred-autoscaler.bccdev.id/wastenot/api/v1/). Penyambungan write API pada Android tetap pekerjaan integrasi terpisah.

Deployment VM dan CI/CD: [panduan cp-bcc](docs/DEPLOYMENT.md).

Runtime YOLO CPU, provenance model dan source Python berada di [ai/](ai/README.md); petunjuk layanan privat ada pada [panduan AI](docs/AI.md).

YOLO11n baseline COCO sudah berjalan di cp-bcc dan upload melalui HTTPS publik telah diuji dengan inferensi nyata. Ini deployment eksperimental, belum validasi akurasi khusus sampah. Metadata saat ini melaporkan `ai_scan=true`, `ai_recommendation=false`; Akses model Gemini sudah diuji secara privat; rekomendasi publik menunggu panduan yang ditinjau. [Bukti dan batas pengujian](docs/VERIFICATION.md).
