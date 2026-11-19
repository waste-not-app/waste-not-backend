# Curated Waste Guides

`core-guides-v1.json` contains **16 articles** covering all seven categories: `PLASTIC`, `ORGANIC`, `PAPER`, `GLASS`, `METAL`, `ELECTRONIC`, and `OTHER`. Each article was checked against primary sources, and the reviewer approved all 16 for publication on 16 November 2026. The articles were later translated from the approved Indonesian text into English without changing their advice, so the next review should confirm the English wording.

## Publication rules

- Only articles with `publication_status: "PUBLISHED"` are served by `/v1/waste-guide`.
- The importer rejects any article that is missing a UUID, a valid enum, steps, HTTPS sources, a reviewer, or a review date.
- Any change to an article's content requires a new review. Do not carry the previous reviewer or date over to unreviewed content.
- Approval covers the article text only. It is not a legal review, a facility inspection, or a guarantee that a local collector accepts the material. Local limits and safety warnings remain part of each article.

## Used by the AI recommendations

The AI recommendation worker builds its guide bundle from this file with `scripts/build_reviewed_guides.py` in [waste-not-ai](https://github.com/waste-not-app/waste-not-ai/tree/main/guides), so scan advice and the Guide screen say the same thing. After any change here, regenerate that bundle and ask the operator to install it, or the two will drift apart.

## Importing

The CD workflow imports the bundle after migrations on every deployment. The import is transactional and upserts by article ID, so redeploys never duplicate the catalog.

To import manually from a source checkout:

```sh
set -a; . ./.env; set +a
go run ./cmd/guides guides/core-guides-v1.json
```

On a deployed host, run the importer that ships inside the image:

```sh
docker compose -p waste-not-backend run --rm --no-deps -T --entrypoint /guides \
  -v "$PWD/guides:/reviewed-guides:ro" api /reviewed-guides/core-guides-v1.json
```

## Sources

Every source below was opened and compared with the article text on 16 November 2026. Rules from sources outside Indonesia are presented as general handling advice, not as local acceptance rules.

| Source | Used for |
|---|---|
| [EPA, How Do I Recycle Common Recyclables][epa-common] | Bottles, resin codes, plastic film, paper and cardboard, glass, cans, and foil |
| [BPOM Tasikmalaya, Choosing Packaging Wisely][bpom] | Why plastic codes alone do not indicate food safety |
| [Cornwall Council, Broken glass][cornwall-glass] | Wrapping and labelling broken glass |
| [EPA, Cleaning Up a Broken CFL][epa-cfl] | Mercury in fluorescent lamps |
| [EPA, Composting At Home][epa-compost] | Aerobic composting, brown-to-green ratio, and excluded materials |
| [EPA, Preventing Wasted Food At Home][epa-food] | Planning and storage to prevent food waste |
| [FDA, Are You Storing Food Safely?][fda-food] | Refrigerator temperature and room-temperature limits |
| [EPA, Used Household Batteries][epa-batteries] | Battery chemistries, terminal isolation, and damaged batteries |
| [Jakarta Provincial Government, E-Waste][jakarta-ewaste] | Electronic waste collection in Jakarta |
| [Jakarta Environment Agency, E-Waste Service][dlh-ewaste] | Drop boxes and pickup requirements |
| [Cornwall Council, Toothbrushes][cornwall-toothbrush] | Plastic toothbrush handling |
| [EPA, Household Hazardous Waste][epa-hhw] | Storing, separating, and handing over hazardous waste |

## Article coverage

| Slug | Sources | Notes |
|---|---|---|
| `pet-plastic-bottles` | [EPA][epa-common], [BPOM][bpom] | Code 1 does not guarantee local acceptance |
| `hdpe-containers` | [EPA][epa-common], [BPOM][bpom], [HHW][epa-hhw] | Previous chemical contents decide separation, not visual cleanliness |
| `reusable-pp-food-containers` | [BPOM][bpom] | Code 5 is not automatically food-safe, reusable, or microwave-safe |
| `plastic-bags-and-wrap` | [EPA][epa-common] | Film needs a dedicated collector; Reduce stays the preferred action |
| `clean-cardboard-boxes` | [EPA][epa-common] | Pizza boxes are recyclable once food residue is removed |
| `office-paper-and-newspapers` | [EPA][epa-common] | Donating books comes first; paper grades are not assumed accepted |
| `intact-glass-bottles-and-jars` | [EPA][epa-common] | Packaging glass only; separate metal lids |
| `broken-glass-and-sharp-shards` | [Cornwall][cornwall-glass], [EPA][epa-common], [CFL][epa-cfl] | Wrap and label; broken lamps are handled differently |
| `aluminium-drink-cans` | [EPA][epa-common], [HHW][epa-hhw] | Do not crush cans for machine sorting; aerosols are excluded |
| `food-cans-and-clean-foil` | [EPA][epa-common] | Confirm acceptance of cans and foil separately |
| `food-scraps-and-composting` | [EPA][epa-compost] | Volume ratio, aeration, and excluded materials |
| `preventing-household-food-waste` | [EPA][epa-food], [FDA][fda-food] | Refrigerate at or below 4 °C; smell and appearance do not prove safety |
| `used-household-batteries` | [EPA][epa-batteries], [DLH][dlh-ewaste] | Not every drop box accepts every battery type or condition |
| `small-electronics-and-cables` | [Jakarta][jakarta-ewaste], [DLH][dlh-ewaste], [EPA][epa-common], [Batteries][epa-batteries] | Wipe personal data; leave built-in batteries in place |
| `manual-plastic-toothbrushes` | [Cornwall][cornwall-toothbrush], [Batteries][epa-batteries] | Electric toothbrushes follow the electronics route |
| `household-hazardous-waste` | [EPA][epa-hhw] | `TRASH` is only a logging category. The article directs users to hazardous waste handlers, not residual waste bins |

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
