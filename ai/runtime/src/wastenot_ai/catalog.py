import json
from importlib.resources import files

from .contracts import Context, RawDetection
from .detection_quality import DEFAULT_PROFILE, annotate_ambiguity, review_profile
from .taxonomy import BROAD_LABELS, BROAD_OBJECT_KEYS


def load_catalog() -> dict:
    return json.loads(files("wastenot_ai").joinpath("data/catalog.json").read_text("utf-8"))


def confirm_context(context: Context) -> dict:
    item = next((x for x in load_catalog()["items"] if x["object_key"] == context.object_key), None)
    if item is None or item["category"] != context.category:
        raise ValueError("Objek/kategori tidak cocok dengan katalog pilot.")
    return item


def preserve_detected_risk(context: Context, selected: dict) -> Context:
    risk = any(item["special_handling"] for item in load_catalog()["items"]
               if item["baseline_class_id"] == selected["class_id"]
               and item["baseline_raw_class"] == selected["raw_class"])
    if not risk:
        return context
    kind = "SHARP" if selected["raw_class"] in {"knife","scissors"} else "ELECTRICAL"
    return context.model_copy(update={"risk_flag":True,
                                      "risk_type":kind if context.risk_type == "UNKNOWN" else context.risk_type})


def _detection_mapping(labels: dict[int, str], catalog: dict):
    # Custom IDs 0..3 are not COCO IDs. Recognize the entire registered taxonomy.
    waste_labels = {0: "aluminium_can", 1: "glass_bottle", 2: "hdpe_bottle", 3: "plastic_bottle"}
    waste_profile = labels == waste_labels
    broad_profile = labels == dict(enumerate(BROAD_LABELS))
    waste_keys = {0: ["aluminium_can"], 1: ["glass_bottle"], 2: ["hdpe_bottle"], 3: ["pet_bottle"]}
    mapping = {}
    for cls, raw in labels.items():
        if broad_profile:
            mapping[cls] = [item for item in catalog["items"] if item["object_key"] in BROAD_OBJECT_KEYS[raw]]
            continue
        mapping[cls] = [item for item in catalog["items"] if (
            item["object_key"] in waste_keys[cls] if waste_profile else
            item["baseline_class_id"] == cls and item["baseline_raw_class"] == raw)]
    version = "broad-waste-objects-01" if broad_profile else (
        "drinking-waste-reviewed-02" if waste_profile else catalog["mapping_version"])
    return version, mapping


def detection_capabilities(labels: dict[int, str], *, model_version=None, model_sha256=None) -> dict:
    version, mapping = _detection_mapping(labels, load_catalog())
    profile = review_profile(labels, model_version, model_sha256)
    if profile["id"] != DEFAULT_PROFILE:
        version += "+" + profile["id"]
    return {"mapping_version": version, "supported_visual_classes": sum(bool(rows) for rows in mapping.values()),
            "supported_object_labels": {item["object_key"]: labels[cls] for cls, rows in mapping.items() for item in rows},
            "review_profile": profile["id"],
            "class_thresholds": {name: {key: value[key] for key in ("lower", "offered")}
                                 for name, value in profile["classes"].items()},
            "review_only_classes": [name for name, value in profile["classes"].items() if not value["offered_enabled"]]}


def review_detections(objects: list[RawDetection], labels: dict[int, str], *, model_version=None, model_sha256=None) -> dict:
    catalog = load_catalog()
    version, mapping = _detection_mapping(labels, catalog)
    thresholds = catalog["thresholds"]
    profile = review_profile(labels, model_version, model_sha256)
    if profile["id"] != DEFAULT_PROFILE:
        version += "+" + profile["id"]
    result = []
    for index, obj in enumerate(objects):
        if labels.get(obj.class_id) != obj.raw_class:
            raise ValueError("Class ID dan label tidak cocok dengan artifact model.")
        matches = mapping[obj.class_id]
        supported = bool(matches)
        selected = profile["classes"].get(obj.raw_class, {})
        limits = {key: selected.get(key, thresholds[key]) for key in ("lower", "offered")}
        reviewable = supported and obj.confidence >= limits["lower"]
        reasons = ["CLASS_REVIEW_ONLY"] if reviewable and not selected.get("offered_enabled", True) else []
        band = "MANUAL_REQUIRED"
        if reviewable:
            band = "OFFERED" if obj.confidence >= limits["offered"] and not reasons else "UNCERTAIN"
        result.append({
            **obj.model_dump(mode="json"), "object_id": f"object-{index + 1}",
            "confidence_band": band, "object_key": None, "category": None,
            "requires_material_confirmation": reviewable,
            "supported_for_review": reviewable,
            "suggested_object_keys": [item["object_key"] for item in matches] if reviewable else [],
            "review_thresholds": limits, "review_reasons": reasons,
        })
    annotate_ambiguity(result, profile["overlap_iou"])
    return {
        "status": "REVIEW_REQUIRED" if any(x["supported_for_review"] for x in result) else "MANUAL_REQUIRED",
        "mapping_version": version,
        "thresholds": thresholds, "review_profile": profile["id"], "objects": result,
    }
