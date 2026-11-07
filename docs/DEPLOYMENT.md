# CI/CD dan deployment blue-green pada cp-bcc

Diperiksa 7 November 2026. Host `okta-mlops`, SSH `proxy.bccdev.id:11049`, alias operator `cp-bcc`. Database `wastenot-db` memakai PostgreSQL18, loopback5432 dan volume persisten. Deploy API tidak mengubah database container/volume atau workload K3s lain.

## Jalur aktif

- Nginx `/wastenot/api/` → `wastenot_upstream` → satu slot Go `127.0.0.1:18081` atau18082.
- Image distroless nonroot; host networking memungkinkan Go memakai worker loopback privat.
- YOLO/Gemini dikelola [repo AI](https://github.com/waste-not-app/waste-not-ai/blob/main/docs/DEPLOYMENT.md), bukan paket backend. Deploy Go tidak merestart worker AI.
- API health publik: `https://pred-autoscaler.bccdev.id/wastenot/api/health/ready`. Body `{"ready":true}` memeriksa core DB/migrasi; header `X-WasteNot-Release` mengidentifikasi commit yang benar-benar melayani request.

## Verifikasi dan promosi image

Satu job menjalankan Go mod verify, pin kontrak AI, tes helper deployment, PostgreSQL18, race detector/vet/format, vulncheck, binary build dan Docker build. Image yang telah diuji disimpan lokal pada runner dan diteruskan ke deployment dalam job yang sama. Alur ini dapat berjalan saat kuota penyimpanan artifact GitHub habis.

Langkah packaging, SSH dan deployment hanya dijalankan untuk main non-PR setelah seluruh pemeriksaan lulus. SSH memakai key khusus dan known-hosts wajib. Image dikirim lewat `docker load`; helper dipasang root:root pada `/usr/local/sbin/wastenot-backend-deploy`. File konfigurasi `/etc/wastenot/wastenot.env` wajib root-owned0600. Helper mem-parsing data konfigurasi, tidak menjalankannya sebagai shell, serta memakai env-file temporer0600 agar credential tidak muncul di argumen proses/log.

Konfigurasi VM memakai DATABASE_URL_APP, DATABASE_URL_MIGRATE, JWT_SECRET_BASE64 serta flag/origin/token AI. `DATABASE_URL` runtime berasal dari DATABASE_URL_APP; migrator hanya menerima DATABASE_URL_MIGRATE. Jangan memberikan credential migrator atau key Gemini kepada API. Capability AI default false bila tidak disetel.

## Transaksi deployment

1. File lock host mencegah dua deploy bersamaan. Tag image harus full commit SHA; helper membaca dan memeriksa image ID target.
2. Simpan **seluruh isi** upstream sebelumnya. Hanya konfigurasi dengan satu slot loopback yang dikenal diterima; jangan menebak slot aktif saat konfigurasi tidak dikenal.
3. Jalankan migrasi memakai image target sebelum startup API. Kegagalan migrasi mempertahankan slot aktif. Migrasi harus kompatibel dengan versi lama yang masih melayani traffic; gunakan expand/contract. Rollback source tidak melakukan down-migration.
4. Sebelum membersihkan slot lain, pastikan slot pada file upstream sehat dan rekonsiliasi Nginx dengan reload. Ini menangani switch yang terputus mendadak; konfigurasi aktif yang tidak sehat menghentikan deploy untuk inspeksi. Drain slot lain45detik sebelum menghapusnya. Bersihkan slot tidak aktif, jalankan target nonroot dengan filesystem read-only, cap-drop ALL, no-new-privileges, memory768MiB, CPU1core, pids256, core dump off dan batas log10MiB×3.
5. Target wajib sehat pada loopback dengan identitas commit yang cocok. Setelah itu tulis upstream atomik, `nginx -t` dan reload.
6. Helper memeriksa route prefiks Nginx. Runner GitHub kemudian memeriksa **HTTPS publik** terhadap ready=true serta header commit baru; VM ini tidak dapat melakukan hairpin ke gateway TLS publiknya sendiri. Jurnal root-only mempertahankan konfigurasi sebelumnya dan slot lama selama menunggu konfirmasi runner. Kegagalan write/validasi/reload/routing mengembalikan konfigurasi upstream persis sebelumnya dan reload sebelum target dibersihkan. Jika reload pemulihan juga gagal, kedua slot dipertahankan agar operator dapat memulihkan routing; jangan mematikan slot yang mungkin masih dipakai Nginx.
7. Runner memanggil `--confirm` hanya setelah gate HTTPS lulus; trap kegagalan memanggil `--rollback`. Jika runner terputus, jurnal tetap ada dan deploy berikutnya memulihkan transaksi pending terlebih dahulu. Pemulihan menyalakan slot sebelumnya dan memeriksa readiness sebelum mengembalikan route, termasuk bila interupsi terjadi setelah drain konfirmasi. Setelah seluruh gate lulus, hentikan slot lama dengan budget45detik, melebihi shutdown aplikasi40detik. Container lama yang berhenti tetap tersedia untuk inspeksi/rollback dan baru dihapus saat slotnya digunakan kembali.

Switch Nginx bersifat graceful, tetapi bukan bukti SLA atau nol request gagal. Belum ada load/drain benchmark pada VM ini. Dua slot sesaat memakai hingga dua pool DB; sisakan koneksi untuk migrasi dan operator.

## Pemeriksaan dan rollback operator

```sh
ssh cp-bcc 'docker ps; cat /etc/nginx/conf.d/wastenot-upstream.conf'
curl -fsS -D - https://pred-autoscaler.bccdev.id/wastenot/api/health/ready
# Jalankan kembali image commit yang sudah tersedia/ditinjau, melalui helper yang sama:
ssh cp-bcc 'sudo -n /usr/local/sbin/wastenot-backend-deploy wastenot-backend:<full-sha>'
# Dari luar VM, periksa ready=true DAN X-WasteNot-Release=<full-sha>:
curl -fsS -D - https://pred-autoscaler.bccdev.id/wastenot/api/health/ready
# Jika sesuai:
ssh cp-bcc 'sudo -n /usr/local/sbin/wastenot-backend-deploy --confirm wastenot-backend:<full-sha>'
# Jika tidak sesuai, gunakan --rollback dengan tag yang sama.
```

Retain image sukses sebelumnya. Image sebelum dukungan header commit memerlukan inspeksi operator untuk pemulihan pertama; helper baru sengaja tidak menganggap health dari versi lama sebagai versi target. Jangan menghapus volume DB untuk memperbaiki deploy API. Backup/restore database tetap pekerjaan terpisah yang harus diuji sebelum rilis luas.

Scripts `deploy.py`, `bootstrap.py`, `install-routing.py`, `wastenot-api.service`, manifest K3s dan `deploy/ai/` merupakan jejak provisioning sebelum pemisahan, bukan pipeline aktif. Jangan menjalankan bootstrap lama pada host Docker aktif. Compose blue-green lama tidak menjalankan transaksi helper dan bukan jalur deploy yang didukung; API hanya dikelola helper di atas. Compose root repo tetap untuk pengembangan core lokal.

Secrets: SSH_PRIVATE_KEY/SSH_KNOWN_HOSTS (fallback WASTENOT_DEPLOY_KEY/WASTENOT_KNOWN_HOSTS). Variables: VM_HOST/VM_PORT/VM_USER (fallback WASTENOT_DEPLOY_HOST/PORT/USER). Secrets tidak dicetak atau disimpan dalam artifact.

[Integrasi Android](ANDROID.md), [operasional](OPERATIONS.md), [bukti verifikasi](VERIFICATION.md).

## Smoke publik setelah deploy

Script operator berikut membuat dua akun fixture, memakai satu generasi Gemini nyata, menguji scan/konteks/ownership/replay/save/provenance/statistik/delete/logout, lalu menghapus hanya akun yang UUID-nya diterima dari registrasi script tersebut. Memerlukan SSH admin ke database Docker; jangan mengarahkannya ke host lain tanpa izin. Foto smoke tidak dikomit atau dikirim ke Gemini.

```sh
python3 scripts/smoke_public.py --ssh cp-bcc --image /path/to/bottle.jpg \
  --output /path/to/smoke-result.json
```

Untuk host tanpa alias SSH, gunakan `--ssh dev@proxy.bccdev.id --ssh-port 11049`. Probe memeriksa identitas dan hash model gabungan v4 + COCO11s. Pengujian release pemulihan dapat mengisi `--expected-model-version` dan `--expected-model-sha256` sesuai manifest model release yang diuji.

Exit0 berarti alur sukses lengkap; exit3 mencatat fallback timeout upstream yang terkendali, bukan keberhasilan alur lengkap. Readiness dan unit test saja tidak menggantikan smoke ini atau penerimaan Android.
