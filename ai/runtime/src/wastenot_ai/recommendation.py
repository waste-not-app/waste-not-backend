from datetime import date
from importlib.resources import files
import json
from pathlib import Path
from typing import Protocol

from pydantic import ValidationError

from .catalog import confirm_context, load_catalog
from .contracts import Context, Guide, ModelSuggestion, Recommendation
from .sources import describe_sources
from .guide_audit import validate_guides


class ProviderError(Exception):
    def __init__(self, code: str):
        self.code = code
        super().__init__(code)


class Provider(Protocol):
    name: str

    def generate(self, payload: dict) -> str: ...


def load_guides(path: Path | None = None) -> list[Guide]:
    text = path.read_text("utf-8") if path else files("wastenot_ai").joinpath("data/guides.json").read_text("utf-8")
    guides = [Guide.model_validate_json(json.dumps(x)) for x in json.loads(text)]
    if path is None:
        templates = json.loads(files("wastenot_ai").joinpath("data/guide_templates.json").read_text("utf-8"))
        for item in load_catalog()["items"]:
            if item["object_key"] in {g.object_key for g in guides} or not item.get("guide_template"):
                continue
            template = templates[item["guide_template"]]
            evidence = [{**e, "id":f"{item['object_key']}-{e['id']}"} for e in template["evidence"]]
            value = {**template,"evidence":evidence,"id":f"demo-{item['object_key']}","version":4,
                     "object_key":item["object_key"],"category":item["category"],"publication_status":"DRAFT",
                     "reviewed_at":None,"reviewer":None,"valid_until":None,
                     "scope_note":"Draf demo berbasis prinsip umum; perlu review penerapan lokal Indonesia.",
                     "special_handling":item["special_handling"]}
            guides.append(Guide.model_validate_json(json.dumps(value)))
    if len({x.object_key for x in guides}) != len(guides):
        raise ValueError("Panduan duplikat untuk object_key.")
    for guide in guides:
        ids = [x.id for x in guide.evidence]
        if len(set(ids)) != len(ids):
            raise ValueError("Evidence ID duplikat.")
    validate_guides(guides)
    return guides


def is_reviewed(guide: Guide, today: date) -> bool:
    if guide.publication_status != "PUBLISHED" or not guide.reviewer or not guide.reviewed_at or not guide.valid_until:
        return False
    try:
        return date.fromisoformat(guide.reviewed_at) <= today <= date.fromisoformat(guide.valid_until)
    except ValueError:
        return False


def eligible_evidence(context: Context, guide: Guide, *, risk: bool = False):
    values = {**context.condition.model_dump(), "reuse_intention": context.reuse_intention,
              "recycling_access": context.recycling_access, "category": context.category.value,
              "object_key": context.object_key}
    washable = (context.condition.cleanliness == "DIRTY" and context.condition.cleanable == "YES"
                and context.condition.soiling in {"FOOD_DRINK","DUST"})
    reusable = ((context.condition.cleanliness == "CLEAN" or washable) and context.condition.integrity == "INTACT"
                and context.condition.reusable == "YES" and context.reuse_intention is True)
    hazardous = (context.risk_type in {"CHEMICAL","BIOLOGICAL","OTHER"} or context.condition.soiling == "HAZARDOUS"
                 or context.condition.former_contents == "CHEMICAL")
    return [e for e in guide.evidence if (e.method != "REUSE" or reusable)
            and (not risk or e.risk_compatible)
            and (context.recommendation_goal != "PREVENTION" or e.method == "REDUCE")
            and (context.recommendation_goal == "PREVENTION" or e.method != "REDUCE")
            and (not hazardous or e.method == "REDUCE")
            and (e.method != "RECYCLE" or context.recycling_access != "NO")
            and all(values.get(k) == v for k, v in e.requires.items())]


def missing_context(context: Context, guide: Guide, *, risk: bool) -> list[str]:
    values = {**context.condition.model_dump(), "reuse_intention":context.reuse_intention,
              "recycling_access":context.recycling_access,"category":context.category.value,"object_key":context.object_key}
    options = []
    for evidence in guide.evidence:
        if evidence.method == "REDUCE" or (risk and not evidence.risk_compatible):
            continue
        if evidence.method == "RECYCLE" and context.recycling_access == "NO":
            continue
        requirements = evidence.requires
        if evidence.method == "REUSE":
            requirements = {"integrity":"INTACT","reusable":"YES","reuse_intention":True,
                            "cleanliness":"CLEAN",**requirements}
        if any(values.get(key) not in {None,"UNKNOWN",expected} for key,expected in requirements.items()):
            continue
        needed = [f"condition.{key}" if key in type(context.condition).model_fields else key
                  for key in requirements if values.get(key) in {None,"UNKNOWN"}]
        if needed:
            options.append(needed)
    return min(options,key=len) if options else []


def conditional_evidence(context: Context, guide: Guide, *, risk: bool):
    values = {**context.condition.model_dump(), "reuse_intention": context.reuse_intention,
              "recycling_access": context.recycling_access, "category": context.category.value, "object_key": context.object_key}
    possible = []
    for evidence in guide.evidence:
        if evidence.method == "REDUCE" or (risk and not evidence.risk_compatible):
            continue
        requirements = evidence.requires
        if evidence.method == "REUSE":
            requirements = {"cleanliness": "CLEAN", "integrity": "INTACT", "reusable": "YES",
                            "reuse_intention": True, **requirements}
        if evidence.method == "RECYCLE" and context.recycling_access == "NO":
            continue
        if any(values.get(key) not in {None, "UNKNOWN", expected} for key, expected in requirements.items()):
            continue
        unknown = {key: expected for key, expected in requirements.items() if values.get(key) in {None, "UNKNOWN"}}
        # Unknown former chemical contents and unknown sharp damage remain safety gates.
        if "former_contents" in unknown or unknown.get("integrity") == "DAMAGED":
            continue
        if unknown:
            possible.append((evidence, unknown))
    return possible


def conditional_text(method, label, unknown):
    descriptions = {
        ("cleanliness", "CLEAN"): "Periksa kebersihan; jalur ini memerlukan benda yang bersih.",
        ("cleanliness", "DIRTY"): "Jika benda kotor, periksa jenis kotorannya sebelum mengikuti langkah pencucian.",
        ("integrity", "INTACT"): "Periksa retak dan kerusakan; gunakan jalur ini hanya bila benda utuh.",
        ("reusable", "YES"): "Pastikan benda masih layak digunakan sesuai fungsinya.",
        ("reusable", "NO"): "Pastikan benda memang tidak lagi layak digunakan sebelum memilih residu.",
        ("reuse_intention", True): "Lanjutkan penggunaan ulang hanya jika kamu ingin memakainya lagi.",
        ("cleanable", "YES"): "Pastikan kotoran dapat dibersihkan dengan cara biasa sesuai petunjuk bahan.",
        ("soiling", "FOOD_DRINK"): "Pastikan kotoran hanya sisa makanan/minuman biasa; jangan gunakan aturan ini untuk kontaminasi berbahaya.",
        ("soiling", "DUST"): "Pastikan kotoran hanya debu biasa; jangan gunakan aturan ini untuk kontaminasi berbahaya.",
        ("recycling_access", "YES"): "Konfirmasikan pengelola yang menerima jenis dan kondisi benda ini sebelum menyerahkan.",
        ("recycling_access", "NO"): "Periksa program pengumpulan yang sesuai; pilih residu hanya jika tidak ada penerima sesuai panduan.",
    }
    labels = {"REUSE": "Reuse", "RECYCLE": "Recycle", "TRASH": "Trash"}
    actions = {"REUSE": "Gunakan kembali hanya setelah kebersihan, keutuhan, kelayakan dan niat pakai ulang dipastikan.",
               "RECYCLE": "Periksa material/kebersihan dan pastikan pengelola menerima benda sebelum menyerahkan.",
               "TRASH": "Jangan langsung membuang; pastikan syarat residu pada panduan terpenuhi terlebih dahulu."}
    reason = f"Saran sementara: {labels[method]} untuk {label}. Informasi yang diperlukan belum lengkap. {actions[method]}"
    steps = list(dict.fromkeys(descriptions.get((key, expected), "Periksa kondisi yang belum diketahui sebelum bertindak.")
                               for key, expected in unknown.items()))
    return reason, steps


def recommend(context: Context, provider: Provider, guides: list[Guide], *,
              demo_guides: bool = False, today: date | None = None) -> Recommendation:
    item = confirm_context(context)
    today = today or date.today()
    guide = next((g for g in guides if g.object_key == context.object_key and g.category == context.category), None)
    prototype = demo_guides or provider.name == "fixture"
    base = {"provider": provider.name, "prototype_only": prototype}

    risk = (item["special_handling"] or context.risk_flag or context.risk_type != "UNKNOWN"
            or context.condition.soiling == "HAZARDOUS" or context.condition.former_contents == "CHEMICAL" or
            ((context.category == "GLASS" or context.object_key.startswith("ceramic_"))
             and context.condition.integrity == "DAMAGED") or
            (guide is not None and guide.special_handling))
    permitted_guide = guide is not None and (is_reviewed(guide, today) or
                                            (demo_guides and guide.publication_status == "DRAFT"))
    caution = guide.caution if permitted_guide else ""
    if risk:
        caution = " ".join(filter(None, ["Ada risiko khusus. Ikuti petunjuk pengemasan dan pengelola yang sesuai; jangan membuang benda berbahaya sembarangan.", caution]))

    def unavailable(code: str, *, missing=None, reason=None):
        urls = guide.source_urls if permitted_guide else []
        if code == "RISK_NOT_COVERED" and (context.risk_type == "CHEMICAL" or context.condition.soiling == "HAZARDOUS"
                                          or context.condition.former_contents == "CHEMICAL"):
            urls = ["https://www.epa.gov/hw/household-hazardous-waste-hhw"]
        return Recommendation(status="NEEDS_CONTEXT" if missing else "UNAVAILABLE",
                              reason=reason or "Rekomendasi otomatis belum tersedia; periksa panduan dan coba lagi setelah konteks atau layanan siap.",
                              reason_code=code, special_handling=bool(risk), caution=caution,
                              guide_refs=[{"guide_id":guide.id,"version":guide.version}] if permitted_guide else [],
                              source_urls=urls, source_details=describe_sources(urls),
                              missing_context=missing or [], **base)

    if not permitted_guide:
        return unavailable("NO_REVIEWED_GUIDE")
    if context.recommendation_goal != "PREVENTION" and context.risk_flag and context.risk_type == "UNKNOWN":
        return unavailable("RISK_DETAILS_REQUIRED", missing=["risk_type"],
                           reason="Risiko khusus dicentang, tetapi jenisnya belum dijelaskan. Pilih jenis risiko agar kotor biasa tidak disamakan dengan kontaminasi berbahaya.")
    evidence = eligible_evidence(context, guide, risk=bool(risk))
    conditional = []
    hazardous = (context.risk_type in {"CHEMICAL", "BIOLOGICAL", "OTHER"} or context.condition.soiling == "HAZARDOUS"
                 or context.condition.former_contents == "CHEMICAL")
    if not evidence and context.allow_conditional and not hazardous and context.recommendation_goal != "PREVENTION":
        conditional = conditional_evidence(context, guide, risk=bool(risk))
        evidence = [row[0] for row in conditional]
    allowed = sorted({e.method for e in evidence})
    if not allowed:
        if (context.risk_type in {"CHEMICAL","BIOLOGICAL","OTHER"} or context.condition.soiling == "HAZARDOUS"
                or context.condition.former_contents == "CHEMICAL"):
            return unavailable("RISK_NOT_COVERED", reason="Panduan benda ini belum mencakup kontaminasi yang kamu laporkan. Jangan gunakan panduan cuci biasa atau buang sebagai residu biasa; minta arahan pengelola limbah yang sesuai.")
        missing = missing_context(context,guide,risk=bool(risk))
        if missing:
            return unavailable("CONTEXT_REQUIRED", missing=missing,
                               reason="Belum cukup informasi untuk memilih penanganan benda saat ini. Lengkapi kondisi yang diketahui; sistem tidak menggantinya dengan saran Reduce.")
        return unavailable("RISK_NOT_COVERED" if risk else "NO_ALLOWED_METHOD")
    payload = {
        "object_key": context.object_key, "category": context.category.value,
        "condition": context.condition.model_dump(), "reuse_intention": context.reuse_intention,
        "recommendation_goal": context.recommendation_goal, "recycling_access": context.recycling_access,
        "special_handling": bool(risk),
        "risk_type": context.risk_type,
        "allowed_methods": allowed,
        "guide": {"id": guide.id, "version": guide.version, "scope_note": guide.scope_note,
                  "evidence": [e.model_dump() for e in evidence], "caution": guide.caution},
    }
    if conditional:
        payload.update(advice_basis="CONDITIONAL", unknown_requirements={e.id: requirements for e, requirements in conditional})
    try:
        raw = ModelSuggestion.model_validate_json(provider.generate(payload))
    except ProviderError as exc:
        return unavailable(exc.code)
    except (ValidationError, ValueError, TypeError):
        return unavailable("INVALID_MODEL_OUTPUT")
    if raw.special_handling != bool(risk) or raw.recommended_method not in allowed:
        return unavailable("INVALID_MODEL_OUTPUT")
    by_id = {e.id: e for e in evidence}
    if len(set(raw.evidence_ids)) != len(raw.evidence_ids) or any(x not in by_id for x in raw.evidence_ids):
        return unavailable("INVALID_EVIDENCE")
    cited = [by_id[x] for x in raw.evidence_ids]
    if {e.method for e in cited} != {raw.recommended_method}:
        return unavailable("INVALID_EVIDENCE")
    # Render public text from validated evidence; free model prose is never a safety authority.
    reason = " ".join(e.text for e in cited)
    verify = {}
    if conditional:
        by_id_unknown = {e.id: requirements for e, requirements in conditional}
        for e in cited:
            for key, expected in by_id_unknown[e.id].items():
                if key in verify and verify[key] != expected:
                    return unavailable("INVALID_EVIDENCE")
                verify[key] = expected
        reason, verification_steps = conditional_text(raw.recommended_method, item["label"], verify)
    if len(reason) > 500:
        return unavailable("INVALID_EVIDENCE")
    source_urls = list(dict.fromkeys(url for e in cited for url in (e.source_urls or guide.source_urls)))
    steps = list(dict.fromkeys(step for e in cited for step in (e.steps or guide.steps)))
    if conditional:
        steps = [*verification_steps, "Ikuti langkah berikut hanya setelah syarat di atas terpenuhi.", *steps]
        caution = " ".join(filter(None, ["Saran bersyarat; informasi yang kosong tidak dianggap sudah terkonfirmasi.", caution]))
    return Recommendation(status="READY", recommended_method=raw.recommended_method,
                          recommendation_basis="CONDITIONAL" if conditional else "CONFIRMED",
                          conditions_to_verify=[f"condition.{key}" if key in type(context.condition).model_fields else key for key in verify],
                          recommendation_scope="PREVENTION" if raw.recommended_method == "REDUCE" else "HANDLING",
                          reason=reason,
                          special_handling=bool(risk), caution=caution, evidence_ids=raw.evidence_ids,
                          source_urls=source_urls, source_details=describe_sources(source_urls),
                          steps=steps,
                          guide_refs=[{"guide_id": guide.id, "version": guide.version}], **base)
