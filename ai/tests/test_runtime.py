import importlib.util
from pathlib import Path
from types import SimpleNamespace

import pytest

from wastenot_ai import providers, recommendation, recommendation_worker


@pytest.mark.parametrize('seconds', [15, 21, 21.01, 120])
def test_recommendation_deadline_fits_go_header_timeout(monkeypatch, seconds):
    spec = importlib.util.spec_from_file_location('runtime_entry', Path(__file__).parents[1] / 'serve.py')
    runtime = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(runtime)
    monkeypatch.setattr(providers, 'GeminiProvider', lambda: SimpleNamespace(timeout=seconds))
    monkeypatch.setattr(recommendation, 'load_guides', lambda _: [])
    monkeypatch.delenv('AI_GUIDES_PATH', raising=False)
    calls = []
    monkeypatch.setattr(recommendation_worker, 'create_recommendation_app', lambda **values: calls.append(values))
    if seconds > 21:
        with pytest.raises(ValueError, match='Go API deadline'):
            runtime.application('recommendation')
        assert calls == []
    else:
        runtime.application('recommendation')
        assert seconds < calls[0]['timeout'] < 23
        assert calls[0]['demo_guides'] is False
