"""Single AI decision interface used by the HTTP adapter, CLI, demo and mock."""
from datetime import date

from .catalog import load_catalog, preserve_detected_risk, review_detections
from .contracts import Category
from .recommendation import is_reviewed, recommend
from .wire import CatalogItem, DecisionInput, ObjectCatalog, RecommendationResult


class DecisionError(ValueError):
    def __init__(self, code):
        self.code = code
        super().__init__(code)


class Decisions:
    def __init__(self, provider, guides, *, demo_guides=False, today=None):
        self.provider, self.guides, self.demo_guides = provider, guides, demo_guides
        self.today = today or date.today

    def evaluate(self, value: DecisionInput) -> RecommendationResult:
        context = value.context
        if value.selection is not None:
            selection = value.selection
            try:
                result = review_detections([selection.object], dict(enumerate(selection.class_names)),
                                          model_version=selection.model_version, model_sha256=selection.model_sha256)
            except ValueError:
                raise DecisionError("INVALID_AI_REFERENCE") from None
            if result["mapping_version"] != selection.mapping_version:
                raise DecisionError("INVALID_AI_REFERENCE")
            selected = result["objects"][0]
            if not selected["supported_for_review"]:
                raise DecisionError("OBJECT_REQUIRES_MANUAL")
            context = preserve_detected_risk(context, selected)
        try:
            result = recommend(context, self.provider, self.guides, demo_guides=self.demo_guides, today=self.today())
        except ValueError:
            raise DecisionError("INVALID_CONTEXT") from None
        return RecommendationResult.model_validate_json(result.model_dump_json())

    def catalog(self) -> ObjectCatalog:
        catalog = load_catalog()
        guides = {guide.object_key: guide for guide in self.guides}
        items = []
        for row in catalog["items"]:
            guide = guides.get(row["object_key"])
            status = "UNAVAILABLE"
            if guide is not None:
                if is_reviewed(guide, self.today()):
                    status = "PUBLISHED"
                elif self.demo_guides and guide.publication_status == "DRAFT":
                    status = "DRAFT"
            items.append(CatalogItem(object_key=row["object_key"], label=row["label"],
                                     category=Category(row["category"]), special_handling=row["special_handling"],
                                     guide_status=status))
        return ObjectCatalog(catalog_version=catalog["catalog_version"], items=items)
