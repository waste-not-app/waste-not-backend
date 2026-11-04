import json

import pytest

from wastenot_ai.contracts import Context
from wastenot_ai.guide_audit import audit_builtin_guides, validate_guides
from wastenot_ai.providers import FixtureProvider
from wastenot_ai.recommendation import load_guides, recommend
from wastenot_ai.sources import load_sources


class CountingProvider(FixtureProvider):
    def __init__(self): self.calls = 0
    def generate(self, payload):
        self.calls += 1
        return super().generate(payload)


@pytest.mark.parametrize("access,status,missing", [("UNKNOWN", "READY", []),
                                                   ("NO", "UNAVAILABLE", []), ("YES", "READY", [])])
def test_pilot_recycling_requires_actual_receiver_and_never_guesses_trash(access, status, missing):
    provider = CountingProvider()
    context = Context.model_validate_json(json.dumps({"object_key": "aluminium_can", "category": "METAL",
            "condition": {"cleanliness": "CLEAN", "integrity": "INTACT"}, "recycling_access": access}))
    result = recommend(context, provider, load_guides(), demo_guides=True)
    assert result.status == status and result.missing_context == missing
    assert result.recommended_method == ("RECYCLE" if status == "READY" else None)
    assert provider.calls == int(status == "READY")
    if access == "UNKNOWN":
        assert result.recommendation_basis == "CONDITIONAL" and result.conditions_to_verify == ["recycling_access"]


@pytest.mark.parametrize("contents,status,risk", [("FOOD_DRINK", "READY", False), ("UNKNOWN", "NEEDS_CONTEXT", False),
                                                ("CHEMICAL", "UNAVAILABLE", True), ("OTHER", "UNAVAILABLE", False)])
def test_hdpe_contents_not_inferred_from_clean_appearance(contents, status, risk):
    provider = CountingProvider()
    context = Context.model_validate_json(json.dumps({"object_key": "hdpe_bottle", "category": "PLASTIC",
            "condition": {"cleanliness": "CLEAN", "integrity": "INTACT", "reusable": "YES", "former_contents": contents},
            "reuse_intention": True, "recycling_access": "YES"}))
    result = recommend(context, provider, load_guides(), demo_guides=True)
    assert result.status == status and result.special_handling == risk
    assert result.recommended_method == ("RECYCLE" if status == "READY" else None)
    assert provider.calls == int(status == "READY")


@pytest.mark.parametrize("requirement", [{"cleanlines": "CLEAN"}, {"cleanliness": "CLEEN"}, {"reuse_intention": "true"}])
def test_bad_guide_conditions_are_rejected_at_load_boundary(requirement):
    guide = load_guides()[0]
    rule = guide.evidence[0].model_copy(update={"requires": requirement})
    with pytest.raises(ValueError):
        validate_guides([guide.model_copy(update={"evidence": [rule]})])


def test_builtin_claim_audit_has_explicit_pilot_sources_and_no_fabricated_publication():
    audit = audit_builtin_guides(load_guides(), load_sources())
    assert audit["guides"] == 78 and audit["publication"]["DRAFT"] == 78
    hdpe = next(guide for guide in audit["pilot_claims"] if guide["object_key"] == "hdpe_bottle")
    assert hdpe["version"] == 5
    recycle = next(rule for rule in hdpe["evidence"] if rule["method"] == "RECYCLE")
    assert recycle["requires"]["former_contents"] == "FOOD_DRINK"
    assert {source["region"] for source in recycle["sources"]} == {"Amerika Serikat", "Indonesia"}
