# Operasional backend

## Build, migrasi dan rilis

`make build` membuat tiga binary statis. Image distroless menjalankan UID nonroot, menyertakan CA certificates; data zona waktu dibundel pada binary API. Entrypoint `/api`, sedangkan `/migrate` dan `/guides` tersedia untuk job operator. Docker Compose menjalankan migrasi sebelum API dengan database persisten. `read_only`, cap_drop ALL dan no-new-privileges aktif pada layanan API.

Migrasi SQL di-embed, urut berdasarkan nama, dilindungi advisory lock transaksi. Nama dan checksum disimpan; perubahan migrasi yang sudah diterapkan ditolak. Tambahkan file baru untuk evolusi schema. Jalankan `/migrate` sekali pada rilis sebelum mengganti instance. Jangan mengembalikan database dengan destructive down migration; gunakan backup/restore dan migrasi perbaikan yang ditinjau. API tidak memigrasikan database otomatis.

Docker Compose hanya menyediakan core lokal. Untuk AI pada container, gunakan network privat dan service origins yang dapat dicapai container; loopback di dalam container bukan host. Jangan membuka port worker ke internet. Set AI_ENABLED lewat deployment yang terkelola setelah gate AI, bukan mengubah Compose lokal menjadi backend publik tanpa TLS.

## Secret dan transport

Buat JWT secret minimal 32 random bytes base64; buat dua token worker berbeda minimal 24 karakter. Simpan pada secret manager atau environment server. Jangan commit `.env`, token, key Gemini, database dump atau credential. Mengganti JWT secret akan meminta semua pengguna login ulang; refresh session DB tetap perlu dicabut saat respons insiden. Key ring/rolling signing-key rotation belum diimplementasikan.

API lokal bind 127.0.0.1:8080. Deployment bersama wajib TLS pada ingress dan TLS database sesuai lingkungan (`sslmode=verify-full` dengan CA yang sesuai). Jangan membiarkan database/worker terbuka publik. Akun database runtime cukup DML terhadap tabel aplikasi; jalankan migrasi/import memakai akun operator dengan hak yang diperlukan.

IP rate limit menggunakan RemoteAddr; forwarded headers diabaikan agar client tidak dapat memalsukan IP. Di balik reverse proxy, batas IP diterapkan pada alamat proxy. Terapkan limit IP pada ingress dan tinjau baseline 20 auth/15 menit; limit identitas dan akun di DB tetap berlaku. Belum ada allowlist trusted proxy. Batas autentikasi default cocok untuk pengembangan/pilot kecil dan perlu dituning sebelum publik.

## Koneksi, kapasitas dan konsistensi

Pool maksimal 10 koneksi per instance; kalikan dengan jumlah instance lalu sisakan koneksi untuk migrasi/operasional. Query timeout 25 detik, deadline request 35 detik, header 5 detik, body 15 detik dan response 40 detik. Shutdown memiliki 40 detik untuk request aktif. Hash password dibatasi empat slot dan decode gambar dua slot per instance; batas ini bukan benchmark kapasitas. Pengujian beban pada hardware tujuan tetap diperlukan.

Riwayat memakai indeks parsial pengguna/tanggal/UUID; tidak memakai offset. Agregat dikerjakan SQL; progress menggunakan count dan tanggal unik. Dashboard memakai repeatable-read snapshot. Create memakai advisory lock scoped user/request plus unique constraint; edit/delete memakai row lock dan version check. Operasi AI melepas transaksi sebelum network provider. CAS pada penyelesaian dan status durable mencegah generasi ganda setelah restart/timeout.

Rate counter tersimpan di PostgreSQL sehingga multi-instance tidak mereset quota. Jendela tetap dapat menghasilkan burst di batas pergantian waktu; ingress/provider budget tetap diperlukan untuk rilis besar. Belum ada global quota Gemini lintas seluruh pengguna/proyek; Python memiliki batas satu job/proses dan pool key-nya sendiri. Jangan menambah replica worker dengan asumsi quota proyek bertambah.

## Retensi, backup dan pemulihan

Cleanup tiap menit: operasi AI expired dihapus, IN_PROGRESS >2 menit menjadi FAILED_UNCERTAIN, rate bucket >2 hari dihapus, sesi expired >7 hari dihapus bersama hash tokennya. Pada downtime, cleanup berjalan lagi setelah API hidup. Tidak menyimpan raw gambar/prompt, query URL, catatan, email atau Authorization pada log. Access log hanya request_id, status, durasi. Log database failure/panic juga tidak mencetak SQL/credential/body.

Waste log soft-delete mempertahankan idempotency key dan snapshot audit; belum ada purge otomatis/account deletion/password reset. Tetapkan kebijakan retensi dan hak penghapusan sebelum rilis publik. Volume/dump/backup database berisi data pribadi dan hash; enkripsi serta batasi akses. Uji restore backup pada database terisolasi. Penghapusan record bukan jaminan terhapus dari backup.

Sesudah crash provider, jangan menjalankan ulang operasi IN_PROGRESS secara otomatis. Cleanup mengubahnya menjadi FAILED_UNCERTAIN; client melihat AI_TIMEOUT untuk ID tersebut. Minta tindakan pengguna baru dengan request ID baru bila perlu generasi ulang. Save waste log yang timeout harus diretry dengan ID dan payload awal karena transaksi mungkin sudah commit.

Health live menunjukkan proses HTTP; ready memeriksa koneksi dan migrasi inti, tanpa detail internal. Monitor request ID/status/durasi, 5xx, 429, DB pool, kegagalan cleanup dan worker secara terpisah. Tidak ada klaim uptime, SLA, throughput atau akurasi model dari tes fungsional ini.
