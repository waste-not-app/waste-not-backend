# Panduan pilot dengan pemeriksaan sumber otomatis

File `pilot-source-reviewed-v6.json` berisi empat panduan PET, HDPE, kaleng aluminium non-aerosol dan kardus. Isinya mengikuti [usulan versi 6](../guide-proposals/README.md) dan [pemeriksaan seluruh sumber](../../docs/AI_GUIDE_SOURCE_REVIEW.md). Pemeriksaan sumber dilakukan Codex; penanggung jawab repo mendelegasikan pemilihan pendekatan. Metadata menyebut **Codex automated source review; not human safety approval**. Ini bukan tanda tangan atau peninjauan keselamatan manusia.

Cakupan: pengguna Indonesia yang mengonfirmasi material, kondisi, riwayat isi bila relevan, dan pengelola setempat yang menerima material/kondisi tersebut. Sumber asing mendukung prinsip bahan, bukan hukum atau penerimaan fasilitas Indonesia. Kaleng aerosol/bertekanan, bekas kimia, dan material di luar cakupan tidak boleh memakai aturan pilot. Label katalog menyebut non-aerosol secara eksplisit. Tidak ada jaminan bahan dari foto atau keamanan isi ulang.

Tanggal pemeriksaan 4 November 2026; masa berlaku sampai 4 Desember 2026 dipilih sebagai siklus tinjauan ulang pilot 30 hari. Setelah kedaluwarsa, policy menolak panduan secara otomatis. Tujuh puluh empat draf lainnya tetap tidak diterbitkan. File draf asli dan proposal tidak diubah menjadi terbitan; artifact ini mempunyai metadata penerbitan terpisah.

Operator memasang file dengan checksum pada path versioned yang hanya dapat dibaca layanan keputusan, kemudian mengisi `AI_GUIDES_PATH`. Tidak memerlukan key Gemini di repo. Worker memvalidasi schema dan status/masa berlaku. `AI_ALLOW_PROTOTYPE` tetap false; provider tetap Gemini asli. Aktifkan capability rekomendasi setelah pemeriksaan worker dan satu alur publik nyata lulus. Respons provider yang lambat/invalid tetap menjadi UNAVAILABLE, tanpa metode buatan server atau penyimpanan otomatis.

Ini pilot integrasi backend. Pemeriksaan sumber otomatis tidak menggantikan holdout model, penilaian manusia untuk penanganan berisiko, mutu/latensi LLM, penerimaan Android, atau persetujuan peluncuran umum.
