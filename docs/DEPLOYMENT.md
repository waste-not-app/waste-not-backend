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

Migrasi dilakukan sebelum perpindahan current. Kegagalan migrasi mempertahankan API sebelumnya. Jika API baru gagal readiness, helper mengaktifkan binary sebelumnya. Rollback binary tidak mengembalikan schema/data; migrasi baru harus kompatibel ke belakang dan backup harus tersedia. Helper menyimpan tiga release terbaru serta current/previous yang dilindungi.

## Bootstrap dan pemeriksaan operator

`deploy/bootstrap.py` adalah bootstrap satu kali untuk VM yang sudah diperiksa; menolak overwrite direktori/namespace/akun WasteNot yang ada. Setelah bootstrap, gunakan prosedur pembaruan sempit, jangan mengulang bootstrap untuk memperbaiki satu file.

```sh
ssh cp-bcc 'sudo systemctl status wastenot-api --no-pager'
ssh cp-bcc 'sudo kubectl -n wastenot get pods,pvc'
ssh cp-bcc 'curl -fsS http://127.0.0.1:18080/health/ready'
ssh cp-bcc 'curl -fsS -H "Host: mlops.bccdev.id" http://127.0.0.1/wastenot/api/health/ready'
ssh cp-bcc 'sudo journalctl -u wastenot-api -n 30 --no-pager'
```

API AI default nonaktif pada deployment ini; tidak ada key/model/panduan terbitan yang difabrikasi. Konten panduan inti tetap perlu diimpor setelah review. Runtime DB tidak memiliki hak CREATE/ALTER schema; role owner dipakai hanya saat migrasi/import. Setelah migrasi awal, hak DML runtime pada schema_migrations dicabut; SELECT tetap cukup untuk readiness.

## Domain dan TLS

Base URL yang dituju Android: `https://mlops.bccdev.id/wastenot/api/v1/`. Status publik harus diverifikasi tersendiri setelah routing/TLS front proxy bekerja; Nginx di VM hanya menerima HTTP origin pada port 80. Jangan mengirim credential Android melalui HTTP publik.

Saat pemeriksaan awal, domain resolve ke `103.179.134.116`, sama dengan gateway SSH. HTTPS dari lokal terputus/reset; VM tidak dapat mencapai port 80/443 domain tersebut. Sementara itu Nginx lokal VM menjawab 200 pada root. Ini menunjukkan forwarding/TLS di depan VM memerlukan verifikasi oleh pengelola gateway; setup backend tidak mengubah DNS, firewall gateway, sertifikat atau routing domain lain.
