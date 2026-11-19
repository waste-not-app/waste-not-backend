# Deployment aktif di alberd-staging

Backend memakai runner self-hosted `alberd-staging-backend` dan Docker Compose
project `waste-not-backend`. API publik: `https://waste-not-api.bccdev.id/v1/`.
`cp-bcc`, prefix `/wastenot/api/`, dan helper blue-green lama bukan target aktif.
Jangan menjalankan bootstrap lama atau menghapus volume database untuk deploy.

## CI dan CD

Workflow `ci.yml` memeriksa source pada pull request. Setelah merge ke `main`,
`cd.yml` membangun image, menjalankan migrasi lewat service `migrate`, lalu
menyalakan API dengan `docker compose -p waste-not-backend up -d --remove-orphans`.
Health check memanggil `http://127.0.0.1:8080/health/ready`.

Konfigurasi runtime utama berasal dari `/home/dev/wastenot-backend.env`. Jika ada,
workflow menambahkan `/home/dev/wastenot-smtp.env` setelahnya, sehingga nilai SMTP
pada file terpisah menjadi prioritas. `.env` sementara memakai umask 077 dan
dihapus lewat trap, termasuk jika build atau import gagal.
Jangan memasukkan credential ke Git, log, argumen proses atau artifact CI.
PostgreSQL memakai volume `postgres-data`, dan API memakai host networking untuk
mengakses database serta worker AI melalui loopback. API tetap nonroot,
filesystem read-only, cap-drop ALL dan no-new-privileges.

## Lampiran foto privat

Compose memasang direktori host persisten pada `/var/lib/wastenot-photos` dan
mengisi `PHOTO_STORAGE_DIR` dengan path container tersebut. Direktori tidak
disajikan oleh Nginx; pembacaan foto tetap melalui endpoint API berautentikasi.
Jangan memakai cache container atau volume sementara untuk lampiran.

Provision sekali pada VM sebelum deploy pertama:

```sh
sudo install -d -o 65532 -g 65532 -m 0700 /var/lib/wastenot-photos
```

`PHOTO_STORAGE_HOST_DIR` dapat mengganti path host. Bind mount sengaja tidak
membuat direktori otomatis: deployment gagal jika provisioning terlewat, bukan
mengaktifkan capability foto pada direktori yang tidak dapat ditulis.
Jangan mengubah kepemilikan direktori yang sudah berisi data tanpa pemeriksaan.

## Pemulihan password

File privat `/home/dev/wastenot-smtp.env` sudah disiapkan dengan mode 0600:

```dotenv
SMTP_HOST=''
SMTP_PORT=587
SMTP_USERNAME=''
SMTP_PASSWORD=''
SMTP_FROM=''
```

Isi host, username, password dan alamat pengirim sesuai layanan SMTP yang nyata.
Gunakan tanda kutip tunggal agar `$` dan `#` pada nilai tidak diinterpolasi Compose;
ikuti aturan escape Compose jika nilai mengandung kutip tunggal. Jangan
mengirim credential lewat chat atau memasukkannya ke repository.

Transport memakai STARTTLS dengan pemeriksaan sertifikat, bukan implicit TLS
port 465. Setelah seluruh nilai terisi, jalankan CD Staging kembali agar
container menerima konfigurasi baru. Tidak ada fallback pengiriman palsu atau
token reset dalam respons HTTP/log. Selama nilai kosong, reset email tetap
belum tersedia; keberhasilan build tidak membuktikan email sampai ke kotak masuk.

## Verifikasi setelah deploy

Periksa health HTTPS, login akun probe terpisah, baca metadata, unggah/ambil foto
privat, dan pastikan akun kedua tidak dapat membacanya. Hapus hanya data probe.
Ulangi pembacaan foto setelah recreating container untuk membuktikan persistensi;
jangan restart database atau menghapus volume.

Backup/restore database dan salinan arsip di host lama memerlukan kebijakan
retensi operator tersendiri. Penghapusan akun di layanan aktif tidak boleh
diklaim otomatis menghapus setiap backup atau arsip deployment lama.

## Bukti verifikasi kode, 16 November 2026

Regresi `Test(PasswordReset|DeleteMe|Photo)` pada `internal/server` lulus dengan
`go test -race -count=1`, memakai PostgreSQL 18 terisolasi, bukan database produksi.
Adapter SMTP dan kesamaan salinan schema juga lulus. Validasi Compose tidak
memerlukan daemon Docker dan sudah lulus.

Lease, eligibility dan backoff antrean email/foto memakai waktu PostgreSQL.
Clock aplikasi tidak dibandingkan dengan default timestamp trigger untuk
menentukan job mana yang siap. Pengujian lintas host menemukan selisih clock
sekitar 650 ms yang sebelumnya menunda pembersihan foto baru dan retry email.
Clock limiter tetap immutable setelah constructor; worker tidak berlomba dengan
inisialisasi handler.

Konfigurasi SMTP privat sudah disiapkan, tetapi kredensial pengirim belum
tersedia sehingga email nyata belum dapat diverifikasi. Enam belas Panduan
disetujui Alberd pada 16 November 2026 dan diimpor secara transaksional oleh CD
setelah migrasi. Lihat `guides/README.md` untuk provenance dan aturan review ulang.
