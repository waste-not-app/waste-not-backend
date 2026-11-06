# Kontrak integrasi

`core.openapi.json` dimiliki backend Go. `android/*.json` berasal dari fixture Android `c1258596cb71bc7b95dc9a4f140f55f759815c4e` dan tetap menjadi acuan transport core awal.

`ai/` adalah snapshot kontrak yang disepakati dengan [waste-not-ai](https://github.com/waste-not-app/waste-not-ai/tree/main/contracts/v1), termasuk tiga OpenAPI, daftar error dan contoh JSON. Generator Python memiliki definisi AI; Go memakai snapshot schema untuk memvalidasi request publik dan respons worker. Kontrak lama yang berasal dari Android diperbarui pada audit 7 November agar cocok dengan runtime terkunci dan metadata sumber saat ini.

[ai.lock.json](ai.lock.json) memuat checksum seluruh 34 artifact dan cocok dengan `contracts/backend-pin.json` di repo AI. CI Go memeriksa bundle terhadap pin. CI AI memeriksa hasil generator dengan `--check`, lalu bundle terhadap pin; CI tidak menulis ulang kontrak agar tes lolos. Pemeriksaan lintas checkout dilakukan sebelum perubahan pasangan diterbitkan:

```sh
python3 scripts/check_contracts.py contracts/ai contracts/ai.lock.json \
  --peer ../waste-not-ai/contracts/v1
```

CI tiap repo tidak mengunduh repo privat lain secara langsung. Pin mengunci bundle yang sudah ditinjau; perubahan API wajib memperbarui source generator, kedua snapshot/pin, contoh, tes Go dan DTO/tes Android yang terpengaruh. Jangan menaikkan pin hanya untuk membungkam kegagalan.

Contoh `provider=fixture`/`prototype_only=true` merupakan bahan pengujian; tidak menjadi hasil produksi. [Integrasi Android](../docs/ANDROID.md) menjelaskan perbedaan status transport, hasil AI, dan aktivitas tersimpan.
