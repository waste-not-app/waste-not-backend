from datetime import date
from pathlib import Path

import pytest

from wastenot_ai.catalog import load_catalog
from wastenot_ai.contracts import Context
from wastenot_ai.providers import FixtureProvider
from wastenot_ai.recommendation import is_reviewed, load_guides, recommend


GUIDES = load_guides(Path(__file__).parents[1] / 'guides/pilot-source-reviewed-v6.json')


class ContractProvider(FixtureProvider):
    name = 'contract-test'


def test_review_metadata_is_honest_and_time_bounded():
    assert len(GUIDES) == 4
    for guide in GUIDES:
        assert guide.reviewer == 'Codex automated source review; not human safety approval'
        assert is_reviewed(guide, date(2026, 11, 4))
        assert is_reviewed(guide, date(2026, 12, 4))
        assert not is_reviewed(guide, date(2026, 11, 3))
        assert not is_reviewed(guide, date(2026, 12, 5))


def test_can_confirmation_label_excludes_aerosols():
    item = next(row for row in load_catalog()['items'] if row['object_key'] == 'aluminium_can')
    assert 'non-aerosol' in item['label'] and item['requires_material_confirmation'] is True


@pytest.mark.parametrize('guide', GUIDES, ids=lambda guide: guide.object_key)
def test_source_reviewed_pilot_can_use_production_policy_without_demo_flag(guide):
    context = Context.model_validate({'object_key': guide.object_key, 'category': guide.category,
        'condition': {'cleanliness': 'CLEAN', 'integrity': 'INTACT', 'former_contents': 'FOOD_DRINK'},
        'recycling_access': 'YES'})
    result = recommend(context, ContractProvider(), GUIDES, today=date(2026, 11, 4))
    assert result.status == 'READY' and result.recommended_method == 'RECYCLE'
    assert result.prototype_only is False and result.provider == 'contract-test'
