# Usulan panduan untuk ditinjau

Audit seluruh 78 draf dan 195 evidence ada di [pemeriksaan sumber](../../docs/AI_GUIDE_SOURCE_REVIEW.md). File [pilot-guides.draft.json](pilot-guides.draft.json) memuat empat kandidat versi 6 dengan perubahan konkret berikut. Semua tetap DRAFT, metadata reviewer/tanggal kosong, dan file ini tidak dipasang pada worker produksi.

| Kandidat | Syarat daur ulang | Perubahan usulan |
|---|---|---|
| Botol PET | Bersih, utuh, bekas pangan/minuman, penerima dikonfirmasi | Riwayat isi wajib; bukan panduan botol bekas kimia atau jaminan isi ulang. |
| Botol HDPE | Bersih, utuh, bekas pangan/minuman, penerima dikonfirmasi | Pertahankan batas isi/kode resin; tidak menjamin layak isi ulang. |
| Kaleng aluminium | Bersih, utuh, bekas pangan/minuman, penerima dikonfirmasi | Batasi ke non-aerosol/tanpa tekanan; pengecualian kimia dan larangan melubangi. |
| Kardus | Bersih, utuh, penerima dikonfirmasi | Syarat kering/minyak mengikuti penerima; tidak menjadikan aturan satu wilayah sebagai aturan universal. |

Masing-masing mempunyai satu evidence RECYCLE dan satu REDUCE. REDUCE hanya untuk tujuan PREVENTION pada pembelian/penggunaan berikutnya, tidak menggantikan penanganan benda saat ini. Material/kondisi/penerimaan tidak disimpulkan dari foto. [EPA bahan umum](https://www.epa.gov/recycle/how-do-i-recycle-common-recyclables), [EPA limbah rumah tangga berbahaya](https://www.epa.gov/hw/household-hazardous-waste-hhw), [BPOM kode resin](https://tasikmalaya.pom.go.id/berita/cerdas-memilih-kemasan-wujudkan-pangan-aman-melalui-pengenalan-kode-plastik).

Tujuh puluh empat panduan lainnya tetap memerlukan perbaikan/review yang dirinci dalam audit, terutama gelas/mangkuk plastik, vas, kaca pecah, elektronik, baterai, pisau/gunting dan penerapan rute asing. Empat kandidat ini tidak menggantikan cakupan audit atau menyatakan seluruh AI selesai.

Sebelum penerbitan, penanggung jawab perlu meninjau isi final, memilih cakupan wilayah, mengisi identitas reviewer serta tanggal/masa berlaku yang nyata. Khusus kaleng, perubahan label katalog/konfirmasi pilot menyebut **“Kaleng aluminium bekas minuman/pangan non-aerosol”**; schema kondisi saat ini tidak mempunyai field khusus aerosol, sehingga konfirmasi cakupan label tetap penting dan file usulan ini tidak dipasang sendiri. Konfirmasi riwayat isi juga harus dapat diberikan Android. Jangan menandai semua draf terbit, mengisi identitas/tanggal review yang tidak benar, atau memakai flag demo pada publik.

Pengujian kandidat memverifikasi schema dan gerbang kondisi: DRAFT ditolak produksi; riwayat isi belum diketahui meminta konteks; akses penerima NO serta kontaminasi kimia tidak menghasilkan saran RECYCLE; input sesuai dapat menjalani jalur prototipe privat. Ini tidak menggantikan peninjauan konten, bukti penerimaan fasilitas, kualitas Gemini atau uji Android. Publikasi dan penyambungan AI_GUIDES_PATH belum dilakukan.

Keputusan pilot berikutnya menggunakan [artifact terpisah dengan pemeriksaan sumber otomatis](../guides/README.md), dengan identitas Codex yang dinyatakan eksplisit. Tidak ada klaim persetujuan keselamatan manusia. File proposal ini tetap DRAFT.
