import json
from io import BytesIO
from types import SimpleNamespace

from PIL import Image

import pytest
from wastenot_ai.catalog import detection_capabilities, review_detections
from wastenot_ai.contracts import RawDetection
from wastenot_ai.decisions import DecisionError, Decisions
from wastenot_ai.providers import FixtureProvider
from wastenot_ai.recommendation import load_guides
from wastenot_ai.wire import DecisionInput, DetectionResult

LABELS = {0: "aluminium_can", 1: "glass_bottle", 2: "hdpe_bottle", 3: "plastic_bottle"}
SHA = "af1446f3333806b0aba3b4618846fe8f95cf5ab3269e2730629b090702498bcf"
IDENTITY = {"model_version": "candidate-af1446f33338", "model_sha256": SHA}


def box(cls, score=.9, bbox=(.1, .1, .8, .9)):
    return RawDetection(class_id=cls, raw_class=LABELS[cls], confidence=score, bbox=bbox)


def typed(result):
    return DetectionResult.model_validate_json(json.dumps({**result, "model_version": IDENTITY["model_version"],
                                                          "image": {"width": 100, "height": 100}}))


def test_competing_classes_stay_selectable_without_suppressing_lower_confidence_truth():
    result = review_detections([box(1, .99), box(0, .8)], LABELS, **IDENTITY)
    assert len(result["objects"]) == 2
    assert all(obj["confidence_band"] == "UNCERTAIN" and obj["supported_for_review"] for obj in result["objects"])
    assert result["objects"][0]["ambiguous_with"] == ["object-2"]
    assert result["objects"][1]["ambiguous_with"] == ["object-1"]
    typed(result)
    result["objects"][1]["ambiguous_with"] = []
    with pytest.raises(ValueError):
        typed(result)


def test_distinct_objects_and_same_class_are_not_competing_hypotheses():
    result = review_detections([box(1), box(0, bbox=(.8, .1, .99, .9)), box(0, bbox=(.8, .1, .99, .9))], LABELS, **IDENTITY)
    assert not any(obj["ambiguous_with"] for obj in result["objects"])
    assert result["objects"][1]["confidence_band"] == "OFFERED"


@pytest.mark.parametrize("cls,score,band", [(1, 1.0, "UNCERTAIN"), (3, .72, "UNCERTAIN"),
                                          (3, .725, "OFFERED"), (0, .7, "OFFERED"), (0, .499, "MANUAL_REQUIRED")])
def test_measured_class_policy_and_boundaries(cls, score, band):
    result = review_detections([box(cls, score)], LABELS, **IDENTITY)
    assert result["objects"][0]["confidence_band"] == band
    assert result["mapping_version"] == "drinking-waste-reviewed-02+dw-val-01"
    typed(result)


def test_profile_is_bound_to_full_checksum_version_and_entire_taxonomy():
    correct = detection_capabilities(LABELS, **IDENTITY)
    assert correct["review_only_classes"] == ["glass_bottle"]
    assert correct["class_thresholds"]["plastic_bottle"]["offered"] == .725
    for change in ({"model_sha256": "f"*64}, {"model_version": "another-model"}, {"model_sha256": None},
                   {"model_version": "candidate-af1446f33338-square640"}):
        value = review_detections([box(1)], LABELS, **{**IDENTITY, **change})
        assert value["review_profile"] == "catalog-default-01"
    assert detection_capabilities({1: "glass_bottle"}, **IDENTITY)["review_profile"] == "catalog-default-01"


def test_private_decision_replays_same_profile_and_rejects_missing_model_provenance():
    selection = {**IDENTITY, "mapping_version": "drinking-waste-reviewed-02+dw-val-01",
                 "class_names": list(LABELS.values()), "object": box(1).model_dump(mode="json")}
    context = {"object_key": "glass_bottle", "category": "GLASS", "recycling_access": "YES",
               "condition": {"cleanliness": "CLEAN", "integrity": "INTACT"}}
    decisions = Decisions(FixtureProvider(), load_guides(), demo_guides=True)
    request = {"context": context, "selection": selection}
    assert decisions.evaluate(DecisionInput.model_validate_json(json.dumps(request))).recommended_method == "RECYCLE"
    selection.pop("model_sha256")
    with pytest.raises(DecisionError, match="INVALID_AI_REFERENCE"):
        decisions.evaluate(DecisionInput.model_validate_json(json.dumps(request)))


@pytest.mark.parametrize("shape,rect", [("adaptive", True), ("square", False)])
def test_detector_passes_explicit_input_shape_to_actual_prediction_seam(shape, rect):
    from wastenot_ai.detector import YoloDetector
    calls = []
    class Model:
        def predict(self, image, **options):
            calls.append(options)
            return [SimpleNamespace(boxes=None)]
    detector = YoloDetector.__new__(YoloDetector)
    detector.model, detector.device, detector.input_shape = Model(), "cpu", shape
    detector.labels, detector.artifact = LABELS, {"model_version":"fixture-model", "sha256":"0"*64}
    content = BytesIO()
    Image.new("RGB", (100, 200), "white").save(content, format="PNG")
    detector.detect(content.getvalue())
    assert calls[0]["rect"] == rect and calls[0]["imgsz"] == 640
