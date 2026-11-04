"""Versioned HTTP models shared by the mock, private adapters and OpenAPI export."""
from datetime import datetime
import json
from typing import Generic, Literal, TypeVar
from uuid import UUID

from pydantic import ConfigDict, Field, field_validator, model_validator

from .contracts import Category, Condition, Context, Contract, RawDetection, Recommendation

API_VERSION = "1.0.0"
POLICY_VERSION = "pilot-policy-06"
T = TypeVar("T")


class Envelope(Contract, Generic[T]):
    api_version: Literal["1.0.0"] = API_VERSION
    request_id: UUID
    data: T


class ErrorDetail(Contract):
    code: str
    message: str
    fields: dict[str, str] = Field(default_factory=dict)
    retryable: bool = False


class ErrorEnvelope(Contract):
    api_version: Literal["1.0.0"] = API_VERSION
    request_id: UUID
    error: ErrorDetail


class Thresholds(Contract):
    lower: float = Field(ge=0, le=1, allow_inf_nan=False)
    offered: float = Field(ge=0, le=1, allow_inf_nan=False)

    @model_validator(mode="after")
    def ordered(self):
        if self.lower > self.offered:
            raise ValueError("Threshold order invalid")
        return self


class ImageDimensions(Contract):
    width: int = Field(ge=1, le=4096)
    height: int = Field(ge=1, le=4096)


class DetectionObject(RawDetection):
    object_id: str = Field(min_length=1, max_length=100)
    confidence_band: Literal["OFFERED", "UNCERTAIN", "MANUAL_REQUIRED"]
    object_key: None = None
    category: None = None
    requires_material_confirmation: bool
    supported_for_review: bool
    suggested_object_keys: list[str] = Field(max_length=100)
    review_thresholds: Thresholds | None = None
    review_reasons: list[Literal["CLASS_REVIEW_ONLY", "OVERLAPPING_CLASSES"]] = Field(default_factory=list, max_length=2)
    ambiguous_with: list[str] = Field(default_factory=list, max_length=49)


class DetectionResult(Contract):
    status: Literal["REVIEW_REQUIRED", "MANUAL_REQUIRED"]
    model_version: str = Field(min_length=1, max_length=100)
    mapping_version: str = Field(min_length=1, max_length=100)
    thresholds: Thresholds
    review_profile: str = Field(default="catalog-default-01", min_length=1, max_length=100)
    image: ImageDimensions
    objects: list[DetectionObject] = Field(max_length=50)

    @model_validator(mode="after")
    def consistent(self):
        reviewable = False
        ids = set()
        for obj in self.objects:
            if obj.object_id in ids:
                raise ValueError("Duplicate object ID")
            ids.add(obj.object_id)
            band = "MANUAL_REQUIRED"
            if obj.supported_for_review:
                limits = obj.review_thresholds or self.thresholds
                if obj.confidence < limits.lower or not obj.suggested_object_keys:
                    raise ValueError("Unsupported review candidate")
                band = "OFFERED" if obj.confidence >= limits.offered and not obj.review_reasons else "UNCERTAIN"
                reviewable = True
            if obj.confidence_band != band or obj.requires_material_confirmation != obj.supported_for_review:
                raise ValueError("Inconsistent review state")
            if not obj.supported_for_review and obj.suggested_object_keys:
                raise ValueError("Manual candidate cannot prefill object keys")
            if len(set(obj.review_reasons)) != len(obj.review_reasons) or len(set(obj.ambiguous_with)) != len(obj.ambiguous_with):
                raise ValueError("Duplicate ambiguity metadata")
            if bool(obj.ambiguous_with) != ("OVERLAPPING_CLASSES" in obj.review_reasons):
                raise ValueError("Ambiguity reason and candidates disagree")
            if obj.review_reasons and not obj.supported_for_review:
                raise ValueError("Manual candidate cannot claim review ambiguity")
        by_id = {obj.object_id: obj for obj in self.objects}
        for obj in self.objects:
            for other_id in obj.ambiguous_with:
                other = by_id.get(other_id)
                if other is None or other_id == obj.object_id or obj.object_id not in other.ambiguous_with or other.class_id == obj.class_id:
                    raise ValueError("Invalid competing hypothesis reference")
        if (self.status == "REVIEW_REQUIRED") != reviewable:
            raise ValueError("Inconsistent detection status")
        return self


class DetectionSession(DetectionResult):
    detection_id: UUID
    expires_at: datetime


class ModelSnapshot(Contract):
    model_version: str = Field(min_length=1, max_length=100)
    sha256: str = Field(pattern=r"^[a-f0-9]{64}$")
    class_names: list[str] = Field(min_length=1, max_length=256)


class InternalDetection(DetectionResult):
    model: ModelSnapshot

    @model_validator(mode="after")
    def label_provenance(self):
        if self.model_version != self.model.model_version:
            raise ValueError("Model versions disagree")
        for obj in self.objects:
            if obj.class_id >= len(self.model.class_names) or self.model.class_names[obj.class_id] != obj.raw_class:
                raise ValueError("Object label disagrees with model snapshot")
        return self


class TrustedSelection(Contract):
    """Go constructs this from its stored Python result, never from a public body."""
    model_version: str = Field(min_length=1, max_length=100)
    mapping_version: str = Field(min_length=1, max_length=100)
    class_names: list[str] = Field(min_length=1, max_length=256)
    model_sha256: str | None = Field(default=None, pattern=r"^[a-f0-9]{64}$")
    object: RawDetection

    @field_validator("object", mode="before")
    @classmethod
    def json_object(cls, value):
        return RawDetection.model_validate_json(json.dumps(value)) if isinstance(value, dict) else value


class DecisionInput(Contract):
    context: Context
    selection: TrustedSelection | None = None

    @field_validator("context", mode="before")
    @classmethod
    def json_context(cls, value):
        return Context.model_validate_json(json.dumps(value)) if isinstance(value, dict) else value


class RecommendationInput(Contract):
    model_config = ConfigDict(json_schema_extra={"allOf": [
        {"if": {"properties": {"input_source": {"const": "DETECTION"}}},
         "then": {"required": ["detection_id", "object_id"], "properties": {
             "detection_id": {"type": "string", "format": "uuid"}, "object_id": {"type": "string", "minLength": 1}}},
         "else": {"properties": {"detection_id": {"type": "null"}, "object_id": {"type": "null"}}}}
    ]})
    client_request_id: UUID = Field(strict=False)
    context_revision: int = Field(default=1, ge=1)
    input_source: Literal["DETECTION", "MANUAL"] = "DETECTION"
    detection_id: UUID | None = Field(default=None, strict=False)
    object_id: str | None = Field(default=None, min_length=1, max_length=100)
    confirmed_object: Literal[True]
    confirmed_object_key: str = Field(min_length=1, max_length=60)
    confirmed_category: Category = Field(strict=False)
    condition: Condition = Condition()
    reuse_intention: bool | None = None
    risk_flag: bool = False
    risk_type: Literal["UNKNOWN", "SHARP", "CHEMICAL", "BIOLOGICAL", "ELECTRICAL", "OTHER"] = "UNKNOWN"
    recommendation_goal: Literal["AUTO", "PREVENTION", "HANDLING"] = "AUTO"
    recycling_access: Literal["YES", "NO", "UNKNOWN"] = "UNKNOWN"
    allow_conditional: bool = True

    @model_validator(mode="after")
    def selection_pair(self):
        if self.input_source == "DETECTION" and (self.detection_id is None or self.object_id is None):
            raise ValueError("Detection source requires both IDs")
        if self.input_source == "MANUAL" and (self.detection_id is not None or self.object_id is not None):
            raise ValueError("Manual source cannot carry detection IDs")
        return self

    def context(self) -> Context:
        return Context(object_key=self.confirmed_object_key, category=self.confirmed_category,
                       condition=self.condition, reuse_intention=self.reuse_intention,
                       risk_flag=self.risk_flag, risk_type=self.risk_type,
                       recommendation_goal=self.recommendation_goal, recycling_access=self.recycling_access,
                       allow_conditional=self.allow_conditional)


class GuideReference(Contract):
    guide_id: str = Field(min_length=1)
    version: int = Field(ge=1)


class SourceReference(Contract):
    url: str
    title: str
    publisher: str
    region: str
    checked_at: str | None
    scope_note: str


class RecommendationResult(Recommendation):
    model_config = ConfigDict(json_schema_extra={"allOf": [
        {"if": {"properties": {"status": {"const": "READY"}}, "required": ["status"]},
         "then": {"required": ["recommended_method", "recommendation_scope", "evidence_ids", "source_urls", "recommendation_basis"],
                  "properties": {"recommended_method": {"enum": ["REDUCE", "REUSE", "RECYCLE", "TRASH"]},
                                 "recommendation_scope": {"enum": ["PREVENTION", "HANDLING"]},
                                 "evidence_ids": {"minItems": 1}, "source_urls": {"minItems": 1},
                                 "recommendation_basis": {"enum": ["CONFIRMED", "CONDITIONAL"]}}},
         "else": {"properties": {"recommended_method": {"type": "null"}, "recommendation_scope": {"type": "null"},
                                  "recommendation_basis": {"type": "null"}, "conditions_to_verify": {"maxItems": 0}}}},
        {"if": {"properties": {"status": {"const": "NEEDS_CONTEXT"}}, "required": ["status"]},
         "then": {"required": ["missing_context"], "properties": {"missing_context": {"minItems": 1}}},
         "else": {"properties": {"missing_context": {"maxItems": 0}}}},
        {"if": {"properties": {"status": {"const": "READY"}, "recommended_method": {"const": "REDUCE"}},
                "required": ["status", "recommended_method"]},
         "then": {"properties": {"recommendation_scope": {"const": "PREVENTION"}}}},
        {"if": {"properties": {"status": {"const": "READY"}, "recommended_method": {"enum": ["REUSE", "RECYCLE", "TRASH"]}},
                "required": ["status", "recommended_method"]},
         "then": {"properties": {"recommendation_scope": {"const": "HANDLING"}}}},
        {"if": {"properties": {"recommendation_basis": {"const": "CONDITIONAL"}}, "required": ["recommendation_basis"]},
         "then": {"required": ["conditions_to_verify"], "properties": {"status": {"const": "READY"}, "conditions_to_verify": {"minItems": 1}}},
         "else": {"properties": {"conditions_to_verify": {"maxItems": 0}}}},
    ]})
    guide_refs: list[GuideReference] = Field(default_factory=list)
    source_details: list[SourceReference] = Field(default_factory=list)
    missing_context: list[Literal["condition.cleanliness", "condition.integrity", "condition.reusable",
                                  "condition.cleanable", "condition.soiling", "condition.former_contents", "reuse_intention",
                                  "recycling_access", "risk_type"]] = Field(default_factory=list)
    conditions_to_verify: list[Literal["condition.cleanliness", "condition.integrity", "condition.reusable",
                                       "condition.cleanable", "condition.soiling", "condition.former_contents",
                                       "reuse_intention", "recycling_access", "risk_type"]] = Field(default_factory=list)


class RecommendationSession(RecommendationResult):
    recommendation_id: UUID
    detection_id: UUID | None
    object_id: str | None
    context_revision: int = Field(ge=1)
    confirmed_object_key: str
    category: Category
    expires_at: datetime


class CatalogItem(Contract):
    object_key: str
    label: str
    category: Category
    special_handling: bool
    guide_status: Literal["DRAFT", "PUBLISHED", "UNAVAILABLE"]


class ObjectCatalog(Contract):
    catalog_version: str
    model_version: str | None = None
    mapping_version: str | None = None
    supported_visual_classes: int | None = Field(default=None, ge=0)
    supported_object_labels: dict[str, str] = Field(default_factory=dict)
    review_profile: str = "catalog-default-01"
    class_thresholds: dict[str, Thresholds] = Field(default_factory=dict)
    review_only_classes: list[str] = Field(default_factory=list)
    items: list[CatalogItem]


def internal_detection(result: dict) -> InternalDetection:
    model = result["model"]
    names = {int(key): value for key, value in model["labels"].items()}
    value = {key: result[key] for key in ("status", "mapping_version", "thresholds", "image", "objects")}
    value["review_profile"] = result.get("review_profile", "catalog-default-01")
    value.update(model_version=model["model_version"], model={
        "model_version": model["model_version"], "sha256": model["sha256"],
        "class_names": [names[i] for i in range(len(names))]})
    return InternalDetection.model_validate_json(json.dumps(value))


class DetectionCapabilities(Contract):
    model: ModelSnapshot
    mapping_version: str
    thresholds: Thresholds
    supported_visual_classes: int = Field(ge=0)
    supported_object_labels: dict[str, str]
    review_profile: str = "catalog-default-01"
    class_thresholds: dict[str, Thresholds] = Field(default_factory=dict)
    review_only_classes: list[str] = Field(default_factory=list)
