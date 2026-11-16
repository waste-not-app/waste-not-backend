# WasteNot Curated Guides (`B11`)

`core-guides-v1.json` berisi **16 artikel** yang mencakup seluruh 7 kategori (`PLASTIC`, `ORGANIC`, `PAPER`, `GLASS`, `METAL`, `ELECTRONIC`, `OTHER`). Penelusuran sumber primer memperbaiki isi dan provenance. Pada 16 November 2026, peninjau **Alberd** menyatakan sudah membaca seluruh 16 draf beserta sumber dan menyetujui publikasinya.

## Aturan Publikasi (`PRD.md` §12 & `API.md` §7)

Seluruh artikel saat ini berstatus `PUBLISHED`, dengan `reviewer: "Alberd"` dan `reviewed_at: "2026-11-16"`, berdasarkan persetujuan eksplisit tersebut. Perubahan isi berikutnya harus ditinjau kembali; jangan mempertahankan metadata review lama untuk isi yang belum disetujui.

CD menjalankan importer transaksional setelah migrasi dan startup API. Upsert memakai ID artikel, sehingga redeploy tidak menggandakan katalog. Untuk menjalankan importer secara manual:

1. Tinjau kesesuaian isi langkah dan batasan lokal di Indonesia untuk setiap artikel.
2. Ganti `reviewer` dengan nama peninjau sebenarnya, perbarui `reviewed_at`, dan ubah `publication_status` ke `"PUBLISHED"`.
3. Jalankan importer:

```sh
set -a; . ./.env; set +a
go run ./cmd/guides guides/core-guides-v1.json
```

Pada VM aktif, image sudah berisi `/guides`. Dari checkout deployment dengan konfigurasi `.env` yang sama, jalankan:

```sh
docker compose -p waste-not-backend run --rm --no-deps -T --entrypoint /guides -v "$PWD/guides:/reviewed-guides:ro" api /reviewed-guides/core-guides-v1.json
```

## Hasil penelusuran sumber, 16 November 2026

Seluruh URL di bawah dibuka dan isi sumber dibandingkan dengan draf. Pemeriksaan sumber otomatis bukan persetujuan manusia. Persetujuan publikasi seluruh artikel diberikan terpisah oleh Alberd pada 16 November 2026; tanggal yang sama kini merupakan tanggal review yang disetujui pada bundle.

Persetujuan ini bukan peninjauan hukum, inspeksi fasilitas, atau jaminan penerimaan material. Batas penerapan lokal dan peringatan keselamatan tetap menjadi bagian artikel.

### Sumber primer

- [EPA, How Do I Recycle Common Recyclables][epa-common]: aturan umum botol, kode resin yang bukan jaminan penerimaan, plastik film, kertas/kardus, kaca, kaleng dan foil. Halaman menegaskan aturan penerima lokal dapat berbeda; ketentuan AS bukan ketentuan fasilitas Indonesia.
- [Balai POM Tasikmalaya, Cerdas Memilih Kemasan][bpom]: kode plastik tidak langsung menunjukkan keamanan pangan; peruntukan, kondisi fisik, suhu, dan petunjuk penggunaan tetap menentukan.
- [Cornwall Council, Broken glass][cornwall-glass]: kemasan kokoh dan label pecahan. Instruksi jadwal/tempat sampah Cornwall tidak diadopsi sebagai aturan Indonesia.
- [EPA, Cleaning Up a Broken CFL][epa-cfl]: lampu fluoresen/CFL mengandung merkuri; bukan jalur yang sama dengan pecahan kaca biasa.
- [EPA, Composting At Home][epa-compost]: kompos aerob memerlukan udara; rasio volume sedikitnya 2–3 bagian cokelat per bagian hijau; bahan yang dihindari dan pematangan.
- [EPA, Preventing Wasted Food At Home][epa-food] dan [FDA, Are You Storing Food Safely?][fda-food]: perencanaan, suhu kulkas, batas waktu suhu ruang, serta ketidakandalan bau/tampilan untuk memastikan keamanan pangan. URL EPA lama `reducing-wasted-food-home` mengarah ke halaman baru `preventing-wasted-food-home`.
- [EPA, Used Household Batteries][epa-batteries]: jenis kimia menentukan jalur, isolasi terminal/kantong terpisah, baterai rusak perlu petunjuk produsen, dan baterai tanam tidak dibongkar paksa.
- [Pemprov DKI Jakarta, Limbah Elektronik][jakarta-ewaste] dan [DLH DKI, Layanan E-Waste][dlh-ewaste]: pengumpulan elektronik dan batas dropbox; layanan jemput berpersyaratan, bukan layanan seluruh Indonesia.
- [Cornwall Council, Toothbrushes][cornwall-toothbrush]: sikat plastik tidak masuk pengumpulan daur ulang biasa di wilayah sumber; jalur lokal tetap harus dikonfirmasi.
- [EPA, Household Hazardous Waste][epa-hhw]: simpan berlabel dalam wadah asal, jangan dicampur atau dituang ke saluran/tanah, wadah berkarat perlu petunjuk khusus, dan hubungi pengelola.

### Cakupan per artikel

| Slug | Dasar bukti dan koreksi |
|---|---|
| `botol-plastik-pet` | [EPA][epa-common] untuk penanganan; [BPOM][bpom] untuk batas penggunaan pangan. Kode 1 bukan jaminan penerima lokal. |
| `wadah-plastik-hdpe` | [EPA][epa-common], [BPOM][bpom], [HHW][epa-hhw]. Riwayat isi kimia menentukan pemisahan, bukan sekadar bersih secara visual. |
| `wadah-makanan-pp-guna-ulang` | [BPOM][bpom]. Tidak semua wadah kode 5 otomatis aman pangan, dapat digunakan berulang, atau aman microwave. |
| `kantong-dan-bungkus-plastik` | [EPA][epa-common]. Film perlu penerima terpisah; tidak menjanjikan toko Indonesia menyediakan layanan yang disebut sumber AS. Reduce tetap tindakan pencegahan. |
| `kardus-karton-bersih` | [EPA][epa-common]. Menghapus aturan mutlak penolakan noda minyak; EPA kini menyebut kotak pizza dapat didaur ulang setelah sisa makanan dikeluarkan. |
| `kertas-hvs-dan-koran` | [EPA][epa-common]. Donasi buku didahulukan; penerimaan jenis kertas tertentu tidak diasumsikan tanpa pengelola. |
| `botol-dan-toples-kaca-utuh` | [EPA][epa-common]. Khusus kemasan; tutup logam dipisah. Menghapus alasan titik leleh yang tidak didukung halaman rujukan. |
| `pecahan-kaca-berisiko-tajam` | [Cornwall][cornwall-glass], [EPA][epa-common], [CFL][epa-cfl]. Kemasan kokoh berlabel; pecahan lampu tidak disamakan dengan kaca biasa. |
| `kaleng-minuman-aluminium` | [EPA][epa-common], [HHW][epa-hhw]. Jangan memadatkan otomatis bila pemilahan mesin; aerosol bukan kaleng minuman. |
| `kaleng-makanan-baja-timah` | [EPA][epa-common]. Kaleng dan foil perlu konfirmasi penerimaan masing-masing; menghapus anjuran memaksa melipat tepi tajam. |
| `sisa-makanan-dan-kompos` | [EPA][epa-compost]. Rasio berdasarkan volume, udara tetap masuk, daftar pengecualian tidak hanya berlaku pada komposter terbuka; bahan dikeringkan bukan otomatis kompos. |
| `pencegahan-sisa-makanan` | [EPA][epa-food], [FDA][fda-food]. Kulkas ≤4 °C; maksimal dua jam suhu ruang, satu jam di atas 32 °C. Bau/tampilan tidak menjamin aman. |
| `baterai-rumah-tangga-bekas` | [EPA][epa-batteries], [DLH][dlh-ewaste]. Tidak semua baterai diklaim mengandung logam berat, tidak semua dropbox menerima semua jenis/kondisi. |
| `perangkat-elektronik-kecil-ewaste` | [Pemprov DKI][jakarta-ewaste], [DLH][dlh-ewaste], [EPA][epa-common], [baterai][epa-batteries]. Hapus data sebelum diserahkan dan pertahankan baterai tanam. |
| `sikat-gigi-plastik-manual` | [Cornwall][cornwall-toothbrush], [baterai][epa-batteries]. Menghapus komposisi kimia universal yang tidak didukung sumber; sikat elektrik memakai jalur elektronik. |
| `limbah-bahan-berbahaya-rumah-tangga` | [EPA][epa-hhw]. Label `TRASH` adalah enum pencatatan yang tersedia, **bukan instruksi membuang B3 ke tong residu**; teks mengarahkan ke petugas/pengelola khusus. |

[epa-common]: https://www.epa.gov/recycle/how-do-i-recycle-common-recyclables
[bpom]: https://tasikmalaya.pom.go.id/berita/cerdas-memilih-kemasan-wujudkan-pangan-aman-melalui-pengenalan-kode-plastik
[cornwall-glass]: https://www.cornwall.gov.uk/rubbish-recycling-and-waste/recycling/recycling-items/broken-glass/
[epa-cfl]: https://www.epa.gov/mercury/cleaning-broken-cfl
[epa-compost]: https://www.epa.gov/recycle/composting-home
[epa-food]: https://www.epa.gov/recycle/preventing-wasted-food-home
[fda-food]: https://www.fda.gov/consumers/consumer-updates/are-you-storing-food-safely
[epa-batteries]: https://www.epa.gov/recycle/used-household-batteries
[jakarta-ewaste]: https://www.jakarta.go.id/e-waste
[dlh-ewaste]: https://lingkunganhidup.jakarta.go.id/layanan/ewaste
[cornwall-toothbrush]: https://www.cornwall.gov.uk/rubbish-recycling-and-waste/recycling/recycling-items/toothbrushes/
[epa-hhw]: https://www.epa.gov/hw/household-hazardous-waste-hhw
