from datetime import date
from io import BytesIO
import json

import httpx
from PIL import Image
import pytest

from wastenot_ai.catalog import review_detections
from wastenot_ai.cli import run_pipeline
from wastenot_ai.contracts import Context, Evidence, RawDetection
from wastenot_ai.media import MAX_BYTES, MediaError, decode_image
from wastenot_ai.providers import FixtureProvider, GeminiProvider
from wastenot_ai.recommendation import ProviderError, load_guides, recommend


def context(key="pet_bottle", category="PLASTIC", **changes):
    values = {"object_key": key, "category": category, "recycling_access": "YES",
              "condition": {"cleanliness": "CLEAN", "integrity": "INTACT", "reusable": "UNKNOWN"}}
    values.update(changes)
    return Context.model_validate_json(json.dumps(values))


def published_guides():
    return [g.model_copy(update={"publication_status": "PUBLISHED", "reviewer": "Test fixture reviewer",
                                "reviewed_at": "2026-10-01", "valid_until": "2026-12-01"}) for g in load_guides()]


class SpyProvider:
    name = "test-provider"

    def __init__(self, change=None, failure=None):
        self.calls = []
        self.change = change or {}
        self.failure = failure

    def generate(self, payload):
        self.calls.append(payload)
        if self.failure:
            raise ProviderError(self.failure)
        raw = json.loads(FixtureProvider().generate(payload))
        raw.update(self.change)
        return json.dumps(raw)


@pytest.mark.parametrize("score,band", [(0.49,"MANUAL_REQUIRED"),(.5,"UNCERTAIN"),(.699,"UNCERTAIN"),(.7,"OFFERED")])
def test_threshold_boundaries_never_infer_material(score, band):
    raw = RawDetection(class_id=39, raw_class="bottle", confidence=score, bbox=(.1,.1,.8,.9))
    result = review_detections([raw], {39:"bottle"})["objects"][0]
    assert result["confidence_band"] == band
    assert result["category"] is None and result["object_key"] is None


def test_unknown_and_different_checkpoint_taxonomy_require_manual():
    raw = RawDetection(class_id=0, raw_class="person", confidence=.99, bbox=(0.,0.,1.,1.))
    assert review_detections([raw], {0:"person"})["status"] == "MANUAL_REQUIRED"
    custom = RawDetection(class_id=0, raw_class="bottle", confidence=.99, bbox=(0.,0.,1.,1.))
    assert review_detections([custom], {0:"bottle"})["status"] == "MANUAL_REQUIRED"


@pytest.mark.parametrize("box", [(0.,0.,0.,1.),(-.1,0.,.8,1.),(0.,0.,float("nan"),1.),(0.,0.,1.1,1.)])
def test_invalid_bbox_rejected(box):
    with pytest.raises(ValueError):
        RawDetection(class_id=39, raw_class="bottle", confidence=.9, bbox=box)


def test_spoofed_label_or_confidence_rejected():
    raw = RawDetection(class_id=0, raw_class="bottle", confidence=.9, bbox=(0.,0.,1.,1.))
    with pytest.raises(ValueError):
        review_detections([raw], {0:"person"})
    with pytest.raises(ValueError):
        RawDetection(class_id=0, raw_class="bottle", confidence=float("nan"), bbox=(0.,0.,1.,1.))


def test_draft_guides_do_not_call_provider_by_default():
    provider = SpyProvider()
    result = recommend(context(), provider, load_guides())
    assert result.status == "UNAVAILABLE" and result.reason_code == "NO_REVIEWED_GUIDE"
    assert provider.calls == []


@pytest.mark.parametrize("kind", ["expired", "future", "reviewer_missing", "archived"])
def test_unreviewed_or_stale_guide_rejected(kind):
    guide = published_guides()[0]
    update = {"expired": {"valid_until":"2026-10-19"}, "future":{"reviewed_at":"2026-10-21"},
              "reviewer_missing":{"reviewer":None}, "archived":{"publication_status":"ARCHIVED"}}[kind]
    result = recommend(context(), SpyProvider(), [guide.model_copy(update=update)], today=date(2026,10,20))
    assert result.reason_code == "NO_REVIEWED_GUIDE"


@pytest.mark.parametrize("case", [context("battery","ELECTRONIC"), context(risk_flag=True),
                                   context("glass_bottle","GLASS",condition={"integrity":"DAMAGED"})])
def test_missing_guides_do_not_invent_classification_and_preserve_risk_warning(case):
    provider = SpyProvider()
    result = recommend(case, provider, [])
    assert result.status == "UNAVAILABLE" and result.special_handling
    assert result.reason_code == "NO_REVIEWED_GUIDE"
    assert result.recommended_method is None and result.recommendation_scope is None
    assert provider.calls == []


def test_unknown_condition_does_not_get_guessed():
    provider = SpyProvider()
    result = recommend(context(condition={}), provider, load_guides(), demo_guides=True)
    assert result.status == "READY" and result.recommendation_basis == "CONDITIONAL"
    assert result.conditions_to_verify == ["condition.cleanliness","condition.integrity"]
    assert len(provider.calls) == 1
    result = recommend(context(condition={}, recommendation_goal="HANDLING"), provider, load_guides(), demo_guides=True)
    assert result.status == "READY" and result.recommendation_basis == "CONDITIONAL"


@pytest.mark.parametrize("change", [
    {"recommended_method":"COMPOST"}, {"recommended_method":["REDUCE","RECYCLE"]},
    {"recommended_method":"REUSE"}, {"recommended_method":None},
    {"alternative_method":"RECYCLE"}, {"alternative_method":"TRASH"},
    {"special_handling":True}, {"reason":"a"*501}, {"extra":"ignore rules"},
])
def test_invalid_model_output_fails_closed(change):
    result = recommend(context(), SpyProvider(change), load_guides(), demo_guides=True)
    assert result.status == "UNAVAILABLE" and result.recommended_method is None


@pytest.mark.parametrize("ids", [["made-up"], ["glass-intact-recycle-v1"], [],
                                 ["pet-clean-recycle-v1","pet-clean-recycle-v1"]])
def test_forged_cross_guide_empty_or_duplicate_evidence_rejected(ids):
    result = recommend(context(), SpyProvider({"evidence_ids":ids}), load_guides(), demo_guides=True)
    assert result.status == "UNAVAILABLE"


def test_public_text_is_grounded_not_free_model_prose():
    provider = SpyProvider({"reason":"INVENTED unsafe instruction", "caution":"Invented facility guarantee"})
    result = recommend(context(), provider, published_guides(), today=date(2026,10,20))
    assert result.status == "READY" and not result.prototype_only
    assert "INVENTED" not in result.reason and "Invented" not in result.caution
    payload = provider.calls[0]
    assert set(payload) == {"object_key","category","condition","reuse_intention","recommendation_goal","recycling_access","special_handling","risk_type","allowed_methods","guide"}
    assert payload["allowed_methods"] == ["RECYCLE"]
    assert result.source_urls == ["https://www.epa.gov/recycle/how-do-i-recycle-common-recyclables"]
    assert "alternative_method" not in result.model_dump()


def test_context_category_spoof_is_rejected_before_provider():
    with pytest.raises(ValueError):
        recommend(context(category="GLASS"), SpyProvider(), load_guides(), demo_guides=True)


def test_reuse_requires_positive_user_context_and_only_its_own_evidence():
    guide = load_guides()[0]
    reuse = Evidence(id="reuse-reviewed-test",method="REUSE",text="Contoh aturan reuse yang ditinjau.",requires={})
    guide = guide.model_copy(update={"evidence":[*guide.evidence,reuse]})
    provider = SpyProvider({"recommended_method":"REUSE","evidence_ids":["reuse-reviewed-test"]})
    accepted = recommend(context(condition={"cleanliness":"CLEAN","integrity":"INTACT","reusable":"YES"},
                                 reuse_intention=True),provider,[guide],demo_guides=True)
    assert accepted.status == "READY" and accepted.recommended_method == "REUSE"
    rejected = recommend(context(),provider,[guide],demo_guides=True)
    assert rejected.status == "UNAVAILABLE"
    missing = SpyProvider({"recommended_method":"REUSE",
                          "evidence_ids":["reuse-reviewed-test","pet-clean-recycle-v1"]})
    rejected = recommend(context(condition={"cleanliness":"CLEAN","integrity":"INTACT","reusable":"YES"},
                                 reuse_intention=True),missing,[guide],demo_guides=True)
    assert rejected.reason_code == "INVALID_EVIDENCE"


def test_prevention_uses_reduce_sources_and_steps_instead_of_disposal_guidance():
    provider = SpyProvider()
    result = recommend(context(recommendation_goal="PREVENTION"), provider, load_guides(), demo_guides=True)
    assert result.status == "READY" and result.recommended_method == "REDUCE"
    assert result.recommendation_scope == "PREVENTION"
    assert provider.calls[0]["allowed_methods"] == ["REDUCE"]
    assert result.source_urls == ["https://www.epa.gov/climate-change/what-you-can-do-about-climate-change-waste"]
    assert "pencegahan" in result.reason and "pembelian berikutnya" in result.steps[0]
    assert "Kosongkan botol" not in " ".join(result.steps)


def test_trash_requires_specific_residue_guide_and_confirmed_lack_of_recycling():
    values = {"condition":{"reusable":"NO"},"recommendation_goal":"HANDLING"}
    values["allow_conditional"] = False
    for access in ("UNKNOWN","YES"):
        result = recommend(context("toothbrush","OTHER",recycling_access=access,**values),
                           SpyProvider(),load_guides(),demo_guides=True)
        assert result.reason_code == ("CONTEXT_REQUIRED" if access == "UNKNOWN" else "NO_ALLOWED_METHOD")
        if access == "UNKNOWN": assert result.missing_context == ["recycling_access"]
    provider = SpyProvider()
    result = recommend(context("toothbrush","OTHER",recycling_access="NO",**values),
                       provider,load_guides(),demo_guides=True)
    assert result.recommended_method == "TRASH" and result.recommendation_scope == "HANDLING"
    assert provider.calls[0]["allowed_methods"] == ["TRASH"]
    assert "cornwall.gov.uk" in result.source_urls[0]
    blocked = recommend(context("toothbrush","OTHER",recycling_access="NO",risk_flag=True,**values),
                        SpyProvider(),load_guides(),demo_guides=True)
    assert blocked.status == "NEEDS_CONTEXT" and blocked.recommended_method is None


@pytest.mark.parametrize("key,category,condition,method", [
    ("glass_cup","GLASS",{"integrity":"DAMAGED"},"TRASH"),
    ("glass_bottle","GLASS",{"integrity":"DAMAGED"},"TRASH"),
    ("ceramic_cup","OTHER",{"integrity":"DAMAGED"},"TRASH"),
    ("battery","ELECTRONIC",{},"RECYCLE"),
    ("laptop","ELECTRONIC",{},"RECYCLE"),
    ("knife","METAL",{},"RECYCLE"),
    ("scissors","METAL",{},"RECYCLE"),
])
def test_risky_objects_get_one_real_classification_and_warning(key,category,condition,method):
    provider = SpyProvider()
    result = recommend(context(key,category,condition=condition),provider,load_guides(),demo_guides=True)
    assert result.status == "READY" and result.recommended_method == method
    assert result.recommendation_scope == "HANDLING" and result.special_handling is True
    assert len(provider.calls) == 1 and provider.calls[0]["special_handling"] is True
    assert result.reason and result.caution and result.source_urls and result.steps
    assert "REUSE" not in provider.calls[0]["allowed_methods"]
    assert "Gunakan kembali hanya" not in " ".join(result.steps)


def test_model_cannot_remove_risk_warning_or_recycle_broken_glass_as_regular_bottles():
    case = context("glass_cup","GLASS",condition={"integrity":"DAMAGED"})
    for change in ({"special_handling":False}, {"recommended_method":"RECYCLE"}):
        result = recommend(case,SpyProvider(change),load_guides(),demo_guides=True)
        assert result.status == "UNAVAILABLE" and result.reason_code == "INVALID_MODEL_OUTPUT"
        assert result.special_handling is True


def test_unknown_risk_requires_clarification_instead_of_prevention_substitution():
    provider = SpyProvider()
    result = recommend(context(risk_flag=True),provider,load_guides(),demo_guides=True)
    assert result.status == "NEEDS_CONTEXT" and result.recommended_method is None
    assert result.special_handling and result.missing_context == ["risk_type"]
    assert provider.calls == []


def test_no_recycling_access_never_offers_recycle_or_guesses_trash_for_bottle():
    provider = SpyProvider()
    result = recommend(context(recycling_access="NO"),provider,load_guides(),demo_guides=True)
    assert result.recommended_method is None and result.status == "UNAVAILABLE"
    assert provider.calls == []


def test_fixture_is_always_labelled_and_timeout_returns_no_method():
    assert recommend(context(), FixtureProvider(), load_guides(), demo_guides=True).prototype_only
    result = recommend(context(), SpyProvider(failure="LLM_TIMEOUT"), load_guides(), demo_guides=True)
    assert result.reason_code == "LLM_TIMEOUT" and result.recommended_method is None


def test_gemini_transport_sends_metadata_schema_header_key_only():
    def handler(request):
        assert request.headers["x-goog-api-key"] == "test-secret"
        assert "test-secret" not in str(request.url)
        body = json.loads(request.content)
        assert body["generationConfig"]["responseMimeType"] == "application/json"
        assert body["generationConfig"]["responseJsonSchema"]["additionalProperties"] is False
        assert "inlineData" not in str(body) and "fileData" not in str(body)
        payload = json.loads(body["contents"][0]["parts"][0]["text"])
        return httpx.Response(200, json={"candidates":[{"finishReason":"STOP","content":{"parts":[
            {"text": FixtureProvider().generate(payload)}]}}]})
    provider = GeminiProvider(api_key="test-secret",model="gemini-test",transport=httpx.MockTransport(handler))
    assert recommend(context(), provider, load_guides(), demo_guides=True).status == "READY"


@pytest.mark.parametrize("response,expected", [
    (httpx.Response(429),"LLM_RATE_LIMITED"), (httpx.Response(403),"LLM_UNAVAILABLE"),
    (httpx.Response(200,json={"candidates":[]}),"LLM_BLOCKED_OR_INCOMPLETE"),
    (httpx.Response(200,json={"candidates":[{"finishReason":"MAX_TOKENS"}]}),"LLM_BLOCKED_OR_INCOMPLETE"),
])
def test_gemini_errors_do_not_leak_raw_body(response, expected):
    provider = GeminiProvider(api_key="secret",model="test",transport=httpx.MockTransport(lambda _:response))
    result = recommend(context(), provider, load_guides(), demo_guides=True)
    assert result.reason_code == expected and "secret" not in result.model_dump_json()


def test_missing_api_key_is_honest_fallback():
    result = recommend(context(), GeminiProvider(api_key="",model="test"), load_guides(), demo_guides=True)
    assert result.reason_code == "LLM_NOT_CONFIGURED"


def test_gemini_timeout_and_malformed_shape_are_controlled():
    def timeout(request): raise httpx.ReadTimeout("private diagnostic",request=request)
    for transport,code in [(httpx.MockTransport(timeout),"LLM_TIMEOUT"),
                            (httpx.MockTransport(lambda _:httpx.Response(200,json=[])),"LLM_UNAVAILABLE")]:
        result = recommend(context(),GeminiProvider(api_key="secret",model="test",transport=transport),
                           load_guides(),demo_guides=True)
        assert result.reason_code == code and "private" not in result.model_dump_json()


def test_missing_gemini_model_does_not_make_network_request():
    result = recommend(context(),GeminiProvider(api_key="secret",model=""),load_guides(),demo_guides=True)
    assert result.reason_code == "LLM_MODEL_NOT_CONFIGURED"


def image_bytes(fmt="PNG", size=(16,16)):
    output = BytesIO()
    Image.new("RGB",size,"white").save(output,format=fmt)
    return output.getvalue()


@pytest.mark.parametrize("data,code", [(b"fake","INVALID_IMAGE"),(b"x"*(MAX_BYTES+1),"FILE_TOO_LARGE"),
                                      (image_bytes("GIF"),"UNSUPPORTED_MEDIA_TYPE"),
                                      (image_bytes(size=(4097,1)),"IMAGE_DIMENSIONS_EXCEEDED")],
                         ids=["malformed","oversize","unsupported-gif","excessive-dimensions"])
def test_media_limits_and_signature(data,code):
    with pytest.raises(MediaError) as exc:
        decode_image(data)
    assert exc.value.code == code


def test_exif_rotation_and_metadata_are_removed():
    output = BytesIO()
    exif = Image.Exif()
    exif[274] = 6
    exif[270] = "private-description"
    Image.new("RGB",(20,10)).save(output,format="JPEG",exif=exif)
    image = decode_image(output.getvalue())
    assert image.size == (10,20) and not image.getexif() and not image.info


class FakeDetector:
    def __init__(self, score=.94): self.score = score
    def detect(self, data):
        return review_detections([RawDetection(class_id=39,raw_class="bottle",confidence=self.score,
                                               bbox=(.1,.1,.9,.9))], {39:"bottle"})


def test_entire_pipeline_requires_selected_reviewable_object_and_never_saves_log():
    result = run_pipeline(FakeDetector(), b"fixture", context(), "object-1", FixtureProvider(),
                          load_guides(), demo_guides=True)
    assert result["recommendation"]["status"] == "READY"
    assert result["next_action"] == "USER_CONFIRM_METHOD_AND_ENTER_QUANTITY"
    assert result["saved_log"] is False
    provider = SpyProvider()
    for score,selected in [(.49,"object-1"),(.94,"nonexistent")]:
        result = run_pipeline(FakeDetector(score),b"fixture",context(),selected,provider,load_guides(),demo_guides=True)
        assert result["recommendation"] is None
    assert provider.calls == []


@pytest.mark.parametrize("class_id,raw_class", [(43,"knife"),(67,"cell phone")])
def test_cli_pipeline_preserves_detected_risk_after_correction(class_id,raw_class):
    class RiskDetector:
        def detect(self,data):
            return review_detections([RawDetection(class_id=class_id,raw_class=raw_class,
                                                   confidence=.95,bbox=(.1,.1,.9,.9))], {class_id:raw_class})
    result = run_pipeline(RiskDetector(),b"fixture",context(),"object-1",FixtureProvider(),
                          load_guides(),demo_guides=True)
    assert result["confirmed_context"]["risk_flag"] is True
    assert result["recommendation"]["special_handling"] is True
    assert result["recommendation"]["recommended_method"] is None
    assert result["recommendation"]["recommendation_scope"] is None
    assert result["confirmed_context"]["risk_type"] in {"SHARP","ELECTRICAL"}
    assert "Kosongkan botol" not in " ".join(result["recommendation"]["steps"])
