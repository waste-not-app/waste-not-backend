# Asal kontrak

Sumber: `waste-not-app/waste-not-android`, commit `c1258596cb71bc7b95dc9a4f140f55f759815c4e`.

| Salinan | Sumber Android |
|---|---|
| `android/*.json` | `app/src/test/resources/contracts/*.json` |
| `ai/*.json` | `ai/contracts/v1/*.json` |
| `ai/examples/*.json` | `ai/contracts/v1/examples/*.json` |

File sumber disalin byte-for-byte. Jangan mengedit hasil ekspor AI untuk menyesuaikan Go; perbarui sumber Python, ekspor ulang, kemudian perbarui snapshot dan pengujian Go. JSON Schema AI dipakai langsung untuk memvalidasi request publik dan respons worker, termasuk aturan conditional status/method/scope/evidence.

`core.openapi.json` mendokumentasikan implementasi Go saat ini. Capability, foto permanen dan achievement mengikuti status yang dijelaskan dalam README. Contoh AI memuat `provider=fixture`/`prototype_only=true`; ini bahan pengujian dan tidak menjadi hasil produksi.
