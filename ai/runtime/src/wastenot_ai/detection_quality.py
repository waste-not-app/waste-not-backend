"""Checkpoint-bound review policy; confidence never confirms material or safety."""
import json
from importlib.resources import files
from .taxonomy import BROAD_LABELS

DEFAULT_PROFILE = "catalog-default-01"


def box_iou(a, b):
    intersection = max(0, min(a[2], b[2])-max(a[0], b[0])) * max(0, min(a[3], b[3])-max(a[1], b[1]))
    union = (a[2]-a[0])*(a[3]-a[1]) + (b[2]-b[0])*(b[3]-b[1]) - intersection
    return intersection / union if union > 0 else 0.0


def review_profile(labels, model_version=None, model_sha256=None):
    if labels == dict(enumerate(BROAD_LABELS)):
        return {"id": "broad-experimental-01", "overlap_iou": .65,
                "classes": {name: {"lower": .5, "offered": .7, "offered_enabled": False}
                            for name in BROAD_LABELS}}
    profile = json.loads(files("wastenot_ai").joinpath("data/detection_profile.json").read_text("utf-8"))
    if (model_sha256 == profile["model_sha256"] and model_version == f"candidate-{model_sha256[:12]}"
            and labels == dict(enumerate(profile["labels"]))):
        return profile
    return {"id": DEFAULT_PROFILE, "classes": {}, "overlap_iou": .65}


def annotate_ambiguity(objects, overlap_iou=.65):
    for index, obj in enumerate(objects):
        competitors = [other["object_id"] for j, other in enumerate(objects) if index != j
                       and obj["supported_for_review"] and other["supported_for_review"]
                       and obj["class_id"] != other["class_id"]
                       and box_iou(obj["bbox"], other["bbox"]) >= overlap_iou]
        obj["ambiguous_with"] = competitors
        if competitors:
            obj["review_reasons"].append("OVERLAPPING_CLASSES")
            obj["confidence_band"] = "UNCERTAIN"
    return objects
