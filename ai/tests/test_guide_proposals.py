from pathlib import Path

import pytest

from wastenot_ai.contracts import Context
from wastenot_ai.providers import FixtureProvider
from wastenot_ai.recommendation import load_guides, recommend


GUIDES = load_guides(Path(__file__).parents[1] / 'guide-proposals/pilot-guides.draft.json')


class Spy(FixtureProvider):
    calls = 0

    def generate(self, payload):
        self.calls += 1
        return super().generate(payload)


def context(guide, **changes):
    values = {'object_key': guide.object_key, 'category': guide.category,
              'condition': {'cleanliness': 'CLEAN', 'integrity': 'INTACT', 'former_contents': 'FOOD_DRINK'},
              'recycling_access': 'YES'}
    values.update(changes)
    return Context.model_validate(values)


def test_candidate_snapshot_is_unpublished():
    assert {guide.object_key for guide in GUIDES} == {'pet_bottle', 'hdpe_bottle', 'aluminium_can', 'cardboard'}
    assert all(guide.publication_status == 'DRAFT' and guide.reviewer is None and guide.reviewed_at is None
               and guide.valid_until is None for guide in GUIDES)


@pytest.mark.parametrize('guide', GUIDES, ids=lambda guide: guide.object_key)
def test_drafts_do_not_generate_in_production(guide):
    provider = Spy()
    result = recommend(context(guide), provider, [guide])
    assert result.status == 'UNAVAILABLE' and result.reason_code == 'NO_REVIEWED_GUIDE'
    assert provider.calls == 0


@pytest.mark.parametrize('guide', GUIDES, ids=lambda guide: guide.object_key)
def test_confirmed_context_recycles_only_as_private_prototype(guide):
    provider = Spy()
    result = recommend(context(guide), provider, [guide], demo_guides=True)
    assert result.status == 'READY' and result.recommended_method == 'RECYCLE' and result.prototype_only is True
    assert provider.calls == 1


@pytest.mark.parametrize('guide', GUIDES, ids=lambda guide: guide.object_key)
@pytest.mark.parametrize('change', [{'recycling_access': 'NO'}, {'risk_type': 'CHEMICAL'}])
def test_rejected_access_or_chemical_risk_never_generates_handling_advice(guide, change):
    provider = Spy()
    result = recommend(context(guide, **change), provider, [guide], demo_guides=True)
    assert result.status != 'READY' and result.recommended_method is None
    assert provider.calls == 0


@pytest.mark.parametrize('guide', [guide for guide in GUIDES if guide.object_key != 'cardboard'],
                         ids=lambda guide: guide.object_key)
def test_unconfirmed_contents_request_context_without_spending_provider_quota(guide):
    provider = Spy()
    value = context(guide, condition={'cleanliness': 'CLEAN', 'integrity': 'INTACT', 'former_contents': 'UNKNOWN'})
    result = recommend(value, provider, [guide], demo_guides=True)
    assert result.status == 'NEEDS_CONTEXT' and 'condition.former_contents' in result.missing_context
    assert provider.calls == 0
