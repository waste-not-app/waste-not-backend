import json

import httpx
import pytest

from wastenot_ai.contracts import Context
from wastenot_ai.providers import GeminiProvider
from wastenot_ai.recommendation import load_guides, recommend


@pytest.mark.parametrize('model', ['gemini-test', 'gemini-3.5-flash-lite'])
@pytest.mark.parametrize('bad', [None, 'method', 'evidence'])
def test_compact_selection_preserves_method_evidence_and_prose_validation(model, bad):
    def handler(request):
        body = json.loads(request.content)
        config = body['generationConfig']
        fields = {'recommended_method', 'special_handling', 'evidence_ids'}
        assert set(config['responseJsonSchema']['properties']) == fields
        assert set(config['responseJsonSchema']['required']) == fields
        assert config['responseJsonSchema']['additionalProperties'] is False
        if model == 'gemini-3.5-flash-lite':
            assert 'temperature' not in config and config['thinkingConfig'] == {'thinkingLevel': 'minimal'}
        else:
            assert config['temperature'] == 0
        payload = json.loads(body['contents'][0]['parts'][0]['text'])
        evidence = next(item for item in payload['guide']['evidence'] if item['method'] == 'RECYCLE')
        choice = {'recommended_method': 'TRASH' if bad == 'method' else 'RECYCLE',
                  'special_handling': False, 'evidence_ids': ['not-retrieved'] if bad == 'evidence' else [evidence['id']]}
        return httpx.Response(200, json={'candidates': [{'finishReason': 'STOP', 'content': {'parts': [{'text': json.dumps(choice)}]}}]})

    context = Context.model_validate_json(json.dumps({'object_key': 'pet_bottle', 'category': 'PLASTIC',
        'recycling_access': 'YES', 'condition': {'cleanliness': 'CLEAN', 'integrity': 'INTACT'}}))
    result = recommend(context, GeminiProvider(api_key='test-only-key', model=model,
                       transport=httpx.MockTransport(handler)), load_guides(), demo_guides=True)
    if bad:
        assert result.status == 'UNAVAILABLE' and result.recommended_method is None
        assert result.reason_code == ('INVALID_MODEL_OUTPUT' if bad == 'method' else 'INVALID_EVIDENCE')
    else:
        assert result.status == 'READY' and result.recommended_method == 'RECYCLE'
        assert result.prototype_only is True
        assert 'Pilihan metode dan evidence untuk validasi server.' not in result.reason
