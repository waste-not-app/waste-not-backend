import json

import pytest

from wastenot_ai.contracts import Context
from wastenot_ai.providers import FixtureProvider
from wastenot_ai.recommendation import load_guides, recommend


def run(key="ceramic_cup", category="OTHER", **changes):
    context = Context.model_validate_json(json.dumps({"object_key": key, "category": category, **changes}))
    return context, recommend(context, FixtureProvider(), load_guides(), demo_guides=True)


def test_empty_ordinary_conditions_produce_conditional_suggestion_without_inventing_facts():
    context, result = run()
    assert result.status == "READY" and result.recommended_method == "REUSE"
    assert result.recommendation_basis == "CONDITIONAL"
    assert set(result.conditions_to_verify) == {"condition.cleanliness", "condition.integrity", "condition.reusable", "reuse_intention"}
    assert "belum" in result.reason.lower() and result.source_urls
    assert context.condition.cleanliness == "UNKNOWN" and context.reuse_intention is None


def test_unknown_recycling_access_is_a_condition_not_a_claim_of_existing_facility():
    _, result = run("pet_bottle", "PLASTIC")
    assert result.status == "READY" and result.recommended_method == "RECYCLE"
    assert result.recommendation_basis == "CONDITIONAL"
    assert "recycling_access" in result.conditions_to_verify
    assert "sudah mengonfirmasi" not in result.reason


@pytest.mark.parametrize("changes", [{"risk_type": "CHEMICAL"}, {"condition": {"soiling": "HAZARDOUS"}}])
def test_conditional_mode_does_not_bypass_reported_hazards(changes):
    _, result = run(**changes)
    assert result.status == "UNAVAILABLE" and result.recommended_method is None


def test_explicit_no_reuse_cannot_be_overridden_by_conditional_suggestion():
    _, result = run(reuse_intention=False)
    assert result.recommended_method != "REUSE"


def test_unknown_chemical_history_for_hdpe_remains_a_safety_question():
    _, result = run("hdpe_bottle", "PLASTIC")
    assert result.status == "NEEDS_CONTEXT" and "condition.former_contents" in result.missing_context


def test_known_ordinary_conditions_still_produce_confirmed_recommendation():
    _, result = run(reuse_intention=True, condition={"cleanliness": "CLEAN", "integrity": "INTACT", "reusable": "YES"})
    assert result.status == "READY" and result.recommendation_basis == "CONFIRMED"
    assert result.conditions_to_verify == []


def test_strict_opt_out_keeps_context_gate_for_integrators_who_need_verified_only():
    _, result = run(allow_conditional=False)
    assert result.status == "NEEDS_CONTEXT" and result.recommendation_basis is None
    assert result.conditions_to_verify == []


def test_model_receives_unknown_facts_separately_from_unverified_guide_requirements():
    class Capturing(FixtureProvider):
        def generate(self, payload):
            self.payload = payload
            return super().generate(payload)
    provider = Capturing()
    context = Context.model_validate_json('{"object_key":"ceramic_cup","category":"OTHER"}')
    result = recommend(context, provider, load_guides(), demo_guides=True)
    assert result.recommendation_basis == "CONDITIONAL"
    assert provider.payload["condition"]["cleanliness"] == "UNKNOWN"
    assert provider.payload["reuse_intention"] is None
    assert provider.payload["advice_basis"] == "CONDITIONAL"
    assert provider.payload["unknown_requirements"]
    assert "sudah menyatakan" not in result.reason


def test_complete_but_contradictory_context_is_not_relaxed_to_obtain_any_method():
    _, result = run("pet_bottle", "PLASTIC", reuse_intention=False, recycling_access="NO",
                    condition={"cleanliness":"CLEAN", "integrity":"INTACT", "reusable":"NO"})
    assert result.status == "UNAVAILABLE" and result.recommended_method is None
