"""Reject unusable evidence conditions before serving a recommendation."""
from typing import get_args
from urllib.parse import urlsplit

from .contracts import Context


def validate_guides(guides):
    fields = {**Context.model_fields, **Context.model_fields["condition"].annotation.model_fields}
    for guide in guides:
        for evidence in guide.evidence:
            for key, value in evidence.requires.items():
                if key not in fields:
                    raise ValueError(f"Unknown evidence requirement: {guide.id}/{evidence.id}/{key}")
                annotation = fields[key].annotation
                permitted = get_args(annotation)
                if permitted and all(isinstance(item, str) for item in permitted):
                    if value not in permitted:
                        raise ValueError(f"Invalid evidence requirement value: {guide.id}/{evidence.id}/{key}")
                elif key in {"reuse_intention", "risk_flag"} and type(value) is not bool:
                    raise ValueError(f"Evidence requirement must be boolean: {key}")
                elif key == "category" and value not in {item.value for item in annotation}:
                    raise ValueError("Invalid evidence category")
            if not (evidence.steps or guide.steps):
                raise ValueError(f"Missing evidence steps: {guide.id}/{evidence.id}")
            for url in [*guide.source_urls, *evidence.source_urls]:
                parsed = urlsplit(url)
                if parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password:
                    raise ValueError(f"Invalid evidence source URL: {guide.id}/{evidence.id}")


def audit_builtin_guides(guides, registry):
    validate_guides(guides)
    unknown = sorted({url for guide in guides for evidence in guide.evidence
                      for url in [*guide.source_urls, *evidence.source_urls] if url not in registry})
    if unknown:
        raise ValueError("Unregistered built-in evidence source")
    pilot = [guide for guide in guides if guide.id.startswith("pilot-") or guide.object_key == "ceramic_cup"]
    return {"guides": len(guides), "evidence": sum(len(g.evidence) for g in guides),
            "sources": len(registry), "publication": {status: sum(g.publication_status == status for g in guides)
                                                       for status in ("DRAFT", "PUBLISHED", "ARCHIVED")},
            "pilot_claims": [{"object_key": g.object_key, "guide_id": g.id, "version": g.version,
                              "scope_note": g.scope_note, "evidence": [
                                  {"id": e.id, "method": e.method, "claim": e.text, "requires": e.requires,
                                   "sources": [registry[url] for url in e.source_urls or g.source_urls],
                                   "steps": e.steps or g.steps} for e in g.evidence]} for g in pilot]}
