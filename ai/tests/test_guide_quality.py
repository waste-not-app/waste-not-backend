from datetime import date
import json

import pytest

from wastenot_ai.contracts import Context
from wastenot_ai.providers import FixtureProvider
from wastenot_ai.recommendation import eligible_evidence, load_guides, recommend
from wastenot_ai.sources import load_sources
from wastenot_ai.guide_audit import audit_builtin_guides, validate_guides


class CountingProvider(FixtureProvider):
    def __init__(self): self.calls = 0
    def generate(self,payload):
        self.calls += 1
        return super().generate(payload)


@pytest.mark.parametrize("source", ["http://example.com/guide", "javascript:alert(1)", "https://user:secret@example.com/guide"])
def test_guide_level_sources_are_checked_even_when_evidence_has_its_own_urls(source):
    guide = load_guides()[0]
    evidence = [e.model_copy(update={"source_urls": ["https://example.com/evidence"]}) for e in guide.evidence]
    guide = guide.model_copy(update={"source_urls": [source], "evidence": evidence})
    with pytest.raises(ValueError, match="Invalid evidence source URL"):
        validate_guides([guide])


def test_builtin_guide_level_sources_must_be_registered_even_if_evidence_overrides_them():
    guide = load_guides()[0]
    guide = guide.model_copy(update={"source_urls": ["https://example.com/unregistered-guide"],
                                   "evidence": [e.model_copy(update={"source_urls": guide.source_urls}) for e in guide.evidence]})
    with pytest.raises(ValueError, match="Unregistered built-in"):
        audit_builtin_guides([guide], load_sources())


def cup(**changes):
    value = {"object_key":"ceramic_cup","category":"OTHER","reuse_intention":True,
             "condition":{"cleanliness":"DIRTY","integrity":"INTACT","reusable":"YES",
                          "soiling":"FOOD_DRINK","cleanable":"YES"},**changes}
    return Context.model_validate_json(json.dumps(value))


@pytest.mark.parametrize("key,category",[("ceramic_cup","OTHER"),("glass_cup","GLASS"),("metal_cup","METAL"),
                                        ("ceramic_bowl","OTHER"),("metal_spoon","METAL"),("metal_fork","METAL")])
@pytest.mark.parametrize("soil",["FOOD_DRINK","DUST"])
def test_washable_dirty_tableware_is_reuse_with_washing_prerequisite(key,category,soil):
    context = cup(object_key=key,category=category)
    context = context.model_copy(update={"condition":context.condition.model_copy(update={"soiling":soil})})
    provider = CountingProvider()
    result = recommend(context,provider,load_guides(),demo_guides=True)
    assert result.status == "READY" and result.recommended_method == "REUSE"
    assert result.recommendation_scope == "HANDLING" and provider.calls == 1
    assert "dicuci" in result.reason and "cuci" in " ".join(result.steps)
    assert context.condition.cleanliness == "DIRTY"
    assert len(result.source_details) == 2
    assert any(source["region"] == "Banjarnegara, Indonesia" for source in result.source_details)


def test_same_mug_with_unclear_risk_requires_explanation_without_calling_model():
    provider = CountingProvider()
    result = recommend(cup(risk_flag=True),provider,load_guides(),demo_guides=True)
    assert result.status == "NEEDS_CONTEXT" and result.missing_context == ["risk_type"]
    assert result.recommended_method is None and provider.calls == 0


@pytest.mark.parametrize("missing",["cleanable","soiling"])
def test_unknown_washing_condition_is_explicit_in_conditional_advice(missing):
    context = cup()
    context = context.model_copy(update={"condition":context.condition.model_copy(update={missing:"UNKNOWN"})})
    provider = CountingProvider()
    result = recommend(context,provider,load_guides(),demo_guides=True)
    assert result.status == "READY" and result.conditions_to_verify == ["condition."+missing]
    assert result.recommendation_basis == "CONDITIONAL" and provider.calls == 1
    assert getattr(context.condition, missing) == "UNKNOWN"


@pytest.mark.parametrize("risk_type",["CHEMICAL","BIOLOGICAL","OTHER"])
def test_hazardous_contamination_never_uses_ordinary_washing_or_trash(risk_type):
    provider = CountingProvider()
    result = recommend(cup(risk_type=risk_type),provider,load_guides(),demo_guides=True)
    assert result.status == "UNAVAILABLE" and result.reason_code == "RISK_NOT_COVERED"
    assert result.special_handling and result.recommended_method is None and provider.calls == 0


def test_hazardous_soiling_cannot_be_hidden_by_unchecked_risk_flag():
    context = cup()
    context = context.model_copy(update={"condition":context.condition.model_copy(update={"soiling":"HAZARDOUS"})})
    result = recommend(context,CountingProvider(),load_guides(),demo_guides=True)
    assert result.reason_code == "RISK_NOT_COVERED" and result.special_handling


def test_explicit_prevention_is_still_reduce_for_same_mug():
    result = recommend(cup(recommendation_goal="PREVENTION"),CountingProvider(),load_guides(),demo_guides=True)
    assert result.status == "READY" and result.recommended_method == "REDUCE"
    assert result.recommendation_scope == "PREVENTION"


def test_all_builtin_rules_have_traceable_sources_valid_conditions_and_steps():
    registry = load_sources()
    valid_keys = {*Context.model_fields,*Context.model_fields["condition"].annotation.model_fields}
    for source in registry.values():
        assert source["url"].startswith("https://") and source["publisher"] and source["region"] and source["scope_note"]
        assert date.fromisoformat(source["checked_at"]) <= date.today()
    for guide in load_guides():
        for evidence in guide.evidence:
            assert set(evidence.requires) <= valid_keys
            assert all(url in registry for url in (evidence.source_urls or guide.source_urls))
            assert evidence.steps or guide.steps
            context = Context(object_key=guide.object_key,category=guide.category)
            assert all(e.method != "REDUCE" for e in eligible_evidence(context,guide))
