# Verifikasi

Tanggal: 4 November 2026. Sumber Android: c1258596cb71bc7b95dc9a4f140f55f759815c4e.

- Go 1.27.3 dan PostgreSQL 18 dipakai secara lokal. Integration suite membuat schema PostgreSQL terisolasi, melakukan migrasi dan menghapus schema setelah selesai.
- 49 kasus/subkasus lulus dengan race detector; tambahan kasus campuran unit penanganan juga lulus dengan race detector. Tidak ada race yang dilaporkan.
- Auth: validasi password/JWT, algoritma/issuer/audience/expiry, login salah, rotasi refresh, replay refresh mencabut sesi, logout idempoten, dan rate limit DB lintas instance.
- Catatan: isolation akun untuk GET/PATCH/DELETE, XOR unit/batas/enum/tanggal, create replay/conflict/deleted replay, delapan create paralel menjadi satu log, version conflict, signed cursor terikat pengguna/filter, dan edit catatan lama.
- Dataset PRD: 1400 g ditangani, 2 benda dihindari, lima aktivitas, recycling rate 50%, 50 XP, streak dua hari. Empty/Reduce-only mengirim rate null. Campuran gram/benda tetap terpisah.
- AI: HTTP worker fixture, model checksum/selection dari snapshot, public field spoofing, replay/conflict, lintas akun, dua instance berbagi satu generasi, timeout tersimpan tanpa retry provider, TTL, batas media/MIME/signature, prototype gate, delete saat generation dan crash recovery/cleanup.
- Worker keputusan Python asli dengan fixture provider/draft guides: enam skenario lulus melalui Go + PostgreSQL + HTTP privat, mencakup empat metode, NEEDS_CONTEXT dan CONDITIONAL. Tidak membaca key Gemini pengguna atau menjalankan inferensi YOLO live.
- Panduan: pencarian PUBLISHED, draft tersembunyi, query injection tidak mengubah SQL, importer menolak reviewer kosong.
- Empat OpenAPI lolos openapi-spec-validator 0.9.0. Respons core pada tes HTTP divalidasi terhadap schema OpenAPI. Empat puluh file snapshot kontrak/fixture cocok byte-for-byte dengan Android sumber.
- go vet, go mod verify, build tiga binary statis, dan pemeriksaan konfigurasi Compose lulus. govulncheck v1.9.0 melaporkan No vulnerabilities found setelah dependency crypto diperbarui ke revisi perbaikan upstream.

Tes ini membuktikan behavior transport/domain dan pengaman transaksi pada lingkungan pengujian. Bukan benchmark beban, audit keamanan menyeluruh, penerimaan UI Android, akurasi YOLO atau mutu Gemini live. Build container dan deployment telah lulus di GitHub Actions run 41813752013. Sepuluh smoke check melalui prefix Nginx di VM lulus; root Nginx dan UID/restart pod sistem dipertahankan. Detail serta batas forwarding/TLS publik ada di DEPLOYMENT.md.

Sepuluh tes boundary/deployment Python mencakup checksum, tar path/symlink/duplikat/limit, migration failure, rollback readiness dan kebutuhan prefix Nginx.
