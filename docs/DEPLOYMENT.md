# CI/CD ke cp-bcc

Target yang diperiksa: host `okta-mlops`, SSH alias lokal `cp-bcc`, endpoint SSH `proxy.bccdev.id:11049`. Hostname tambahan `proxy.bcc.dev.id` yang disebut pada pesan tidak resolve saat verifikasi; setup memakai alias SSH yang sudah berhasil masuk.

## Batas perubahan VM

WasteNot memakai namespace K3s `wastenot` untuk PostgreSQL 18 dan PVC 2 GiB, tanpa port database publik. ResourceQuota dan NetworkPolicy berlaku hanya dalam namespace ini. Pod memakai user nonroot, capability kosong dan service-account token nonaktif. Database owner `wastenot_owner` terpisah dari role runtime `wastenot_app`; password dan JWT dibuat acak pada VM, tidak ditaruh di repo/CI.

Go dijalankan sebagai user `wastenot` pada `wastenot-api.service`, bind `127.0.0.1:18080`. Batas API: MemoryMax 512 MiB, CPUQuota 100%, nice 10, filesystem read-only dan no-new-privileges. Migrasi memakai akun OS `wastenot-migrate` serta file environment owner terpisah. Secret root-only berada di `/etc/wastenot`; source release di `/opt/wastenot/releases/<commit>`. Symlink current/previous menunjuk rilis aktif/rollback.

Nginx hanya menambah include lokasi `/wastenot/api/`, dengan proxy prefix ke API lokal. `/wastenot/api/v1/me` menjadi `/v1/me`; `/wastenot/api/health/ready` menjadi `/health/ready`. Tidak mengganti server root/default atau restart K3s. Sebelum include dipasang, simpan backup file Nginx yang ada, jalankan `nginx -t`, lalu reload Nginx secara graceful. Upload tidak dibuffer ke disk dan write tidak diretry proxy.

## Pipeline

`.github/workflows/ci.yml` menjalankan PostgreSQL integration/race tests, deployment boundary/rollback tests, vet, go mod verify, pemeriksaan kerentanan, build binary dan image Docker. Go dipin 1.27.3 agar gate keamanan tidak memakai patch standard library lama.

Setelah verify sukses pada main, paket tiga binary statis dan release.json diunggah sebagai artifact dengan SHA-256. Job deploy mengunduh artifact dari run yang sama lalu mengirim tar melalui SSH. Push main tidak membatalkan deploy yang sedang berlangsung. Pull request tidak dapat menjalankan deploy atau memperoleh key lewat job ini.

Secrets repository:

- `WASTENOT_DEPLOY_KEY`: key baru khusus CI, berbeda dari key SSH personal pengguna.
- `WASTENOT_KNOWN_HOSTS`: host key yang dibaca melalui sesi SSH awal yang sudah terautentikasi; tidak melakukan trust-on-first-use di CI.

Variables: `WASTENOT_DEPLOY_HOST`, `WASTENOT_DEPLOY_PORT`, `WASTENOT_DEPLOY_USER`.

Akun `wastenot-ci` hanya menerima forced command root `/usr/local/sbin/wastenot-deploy`, tanpa forwarding/PTY dan tanpa shell bebas. Helper root menolak tar compressed, anggota tambahan/duplikat/symlink/path escape, archive >80 MiB, binary >24 MiB dan checksum/ELF/commit invalid. Helper hanya memperbarui direktori/layanan WasteNot; binary dan migrasi berjalan sebagai user layanan, bukan root. Helper/infrastruktur tidak ikut ditimpa oleh paket CI; pembaruannya melalui sesi admin yang ditinjau.

Migrasi dilakukan sebelum perpindahan current. Kegagalan migrasi mempertahankan API sebelumnya. Jika API baru gagal readiness langsung maupun prefix Nginx yang sudah dipasang, helper mengaktifkan binary sebelumnya. Rollback binary tidak mengembalikan schema/data; migrasi baru harus kompatibel ke belakang dan backup harus tersedia. Helper menyimpan tiga release terbaru serta current/previous yang dilindungi.

## Bootstrap dan pemeriksaan operator

`deploy/bootstrap.py` adalah bootstrap satu kali untuk VM yang sudah diperiksa; menolak overwrite direktori/namespace/akun WasteNot yang ada. Setelah bootstrap, gunakan prosedur pembaruan sempit, jangan mengulang bootstrap untuk memperbaiki satu file.

```sh
ssh cp-bcc 'sudo systemctl status wastenot-api --no-pager'
ssh cp-bcc 'sudo kubectl -n wastenot get pods,pvc'
ssh cp-bcc 'curl -fsS http://127.0.0.1:18080/health/ready'
ssh cp-bcc 'curl -fsS -H "Host: pred-autoscaler.bccdev.id" http://127.0.0.1/wastenot/api/health/ready'
ssh cp-bcc 'sudo journalctl -u wastenot-api -n 30 --no-pager'
```

API AI default nonaktif pada deployment ini; tidak ada key/model/panduan terbitan yang difabrikasi. Konten panduan inti tetap perlu diimpor setelah review. Runtime DB tidak memiliki hak CREATE/ALTER schema; role owner dipakai hanya saat migrasi/import. Setelah migrasi awal, jalankan deploy/harden-db.sql melalui psql operator untuk mencabut hak DML runtime pada schema_migrations; SELECT tetap cukup untuk readiness. deploy/install-routing.py memasang include dengan backup, pemeriksaan root tetap identik, penantian reload worker dan rollback konfigurasi bila gagal.

## Domain dan TLS

Base URL yang dituju Android: `https://pred-autoscaler.bccdev.id/wastenot/api/v1/`. HTTPS dan prefix publik sudah terverifikasi; Nginx di VM menerima HTTP origin pada port 80 di belakang gateway yang menangani TLS. Jangan mengirim credential Android melalui HTTP publik.

Domain awal `mlops.bccdev.id` merupakan kekeliruan alamat dan sudah dikoreksi pengguna menjadi `pred-autoscaler.bccdev.id`. Domain yang benar mempunyai TLS valid dan endpoint readiness publik mengembalikan 200 dengan ready=true. Tidak ada perubahan DNS, sertifikat gateway atau routing domain lain. Setelah deploy, GitHub Actions juga memeriksa readiness melalui HTTPS publik dari runner.


## Bukti deployment 4 November 2026

Run [41813752013](https://github.com/waste-not-app/waste-not-backend/actions/runs/41813752013) meluluskan verify dan deploy commit 2b9b2b6. API hidup sebagai wastenot dengan database runtime nonsuperuser. Sepuluh pemeriksaan HTTP melalui Nginx VM lulus: readiness prefix, register, metadata, create, replay, edit versioned, statistik, delete, XP dan logout. Akun smoke sementara dibersihkan dengan ID-nya sendiri; tidak ada akun seed.

Backup Nginx sebelum include: /var/backups/wastenot/nginx-default.20261104T102847Z.before. Hash sumber sebelum ce0901350a021608139b5639cf4ccd7717bef8c3a9e4f79031eb46386b67b03f dan sesudah 83a7a4e67a9fd244a1aa840ccbf46f35b457191afcb25548c2e7e3fddb80858b. Isi root HTTP sama, master PID Nginx tetap 3088285, dan tiga pod sistem K3s mempertahankan UID serta restart count nol. File routing-verification.json di direktori backup menyimpan hasil.

Bukti ini menyatakan jalur GitHub Actions → SSH khusus → migrasi → service Go → Nginx VM berfungsi. HTTPS publik melalui domain yang dikoreksi sudah diuji terpisah, termasuk sepuluh langkah auth/pencatatan/statistik/logout dengan akun sementara yang kemudian dibersihkan.
