from enum import StrEnum
import math
from typing import Literal
from typing import Annotated

from pydantic import BaseModel, ConfigDict, Field, model_validator


class Contract(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True, frozen=True)


class Category(StrEnum):
    ORGANIC = "ORGANIC"
    PLASTIC = "PLASTIC"
    PAPER = "PAPER"
    GLASS = "GLASS"
    METAL = "METAL"
    ELECTRONIC = "ELECTRONIC"
    OTHER = "OTHER"


Method = Literal["REDUCE", "REUSE", "RECYCLE", "TRASH"]


class Condition(Contract):
    cleanliness: Literal["CLEAN", "DIRTY", "UNKNOWN"] = "UNKNOWN"
    integrity: Literal["INTACT", "DAMAGED", "UNKNOWN"] = "UNKNOWN"
    reusable: Literal["YES", "NO", "UNKNOWN"] = "UNKNOWN"
    cleanable: Literal["YES", "NO", "UNKNOWN"] = "UNKNOWN"
    soiling: Literal["FOOD_DRINK", "DUST", "HAZARDOUS", "UNKNOWN"] = "UNKNOWN"
    former_contents: Literal["FOOD_DRINK", "CHEMICAL", "OTHER", "UNKNOWN"] = "UNKNOWN"


class Context(Contract):
    object_key: str = Field(min_length=1, max_length=60)
    category: Category
    condition: Condition = Condition()
    reuse_intention: bool | None = None
    risk_flag: bool = False
    risk_type: Literal["UNKNOWN", "SHARP", "CHEMICAL", "BIOLOGICAL", "ELECTRICAL", "OTHER"] = "UNKNOWN"
    recommendation_goal: Literal["AUTO", "PREVENTION", "HANDLING"] = "AUTO"
    recycling_access: Literal["YES", "NO", "UNKNOWN"] = "UNKNOWN"
    allow_conditional: bool = True


Coordinate = Annotated[float, Field(ge=0, le=1, allow_inf_nan=False)]


class RawDetection(Contract):
    class_id: int = Field(ge=0)
    raw_class: str = Field(min_length=1, max_length=100)
    confidence: float = Field(ge=0, le=1, allow_inf_nan=False)
    bbox: tuple[Coordinate, Coordinate, Coordinate, Coordinate]

    @model_validator(mode="after")
    def check_box(self):
        x1, y1, x2, y2 = self.bbox
        if not all(math.isfinite(x) and 0 <= x <= 1 for x in self.bbox):
            raise ValueError("Bbox harus finite dan normalized 0..1.")
        if x1 >= x2 or y1 >= y2:
            raise ValueError("Bbox harus memiliki luas positif.")
        return self


class ModelSuggestion(Contract):
    recommended_method: Method
    reason: str = Field(min_length=1, max_length=500)
    special_handling: bool
    caution: str = Field(max_length=500)
    evidence_ids: list[str] = Field(min_length=1, max_length=10)


class Evidence(Contract):
    id: str
    method: Method
    text: str = Field(min_length=1, max_length=500)
    requires: dict[str, str | bool]
    source_urls: list[str] = Field(default_factory=list)
    steps: list[str] = Field(default_factory=list)
    risk_compatible: bool = False


class Guide(Contract):
    id: str
    version: int = Field(ge=1)
    object_key: str
    category: Category
    publication_status: Literal["DRAFT", "PUBLISHED", "ARCHIVED"]
    reviewed_at: str | None
    reviewer: str | None
    valid_until: str | None
    source_urls: list[str] = Field(min_length=1)
    scope_note: str
    special_handling: bool
    steps: list[str]
    caution: str = Field(max_length=500)
    evidence: list[Evidence]


class Recommendation(Contract):
    status: Literal["READY", "NEEDS_CONTEXT", "UNAVAILABLE"]
    recommended_method: Method | None = None
    recommendation_scope: Literal["PREVENTION", "HANDLING"] | None = None
    reason: str
    special_handling: bool = False
    caution: str = ""
    evidence_ids: list[str] = Field(default_factory=list)
    guide_refs: list[dict] = Field(default_factory=list)
    source_urls: list[str] = Field(default_factory=list)
    source_details: list[dict] = Field(default_factory=list)
    steps: list[str] = Field(default_factory=list)
    reason_code: str | None = None
    missing_context: list[str] = Field(default_factory=list)
    recommendation_basis: Literal["CONFIRMED", "CONDITIONAL"] | None = None
    conditions_to_verify: list[str] = Field(default_factory=list)
    recommendation_version: str = "pilot-policy-06"
    provider: str
    prototype_only: bool

    @model_validator(mode="after")
    def check_status(self):
        if (self.status == "NEEDS_CONTEXT") != bool(self.missing_context):
            raise ValueError("NEEDS_CONTEXT wajib menyebut isian yang kurang; status lain tidak memilikinya.")
        if self.status != "READY" and (self.recommended_method or self.recommendation_scope):
            raise ValueError("Fallback harus method dan scope null.")
        if self.status == "READY" and not self.recommended_method:
            raise ValueError("READY membutuhkan satu metode.")
        if self.status == "READY":
            expected = "PREVENTION" if self.recommended_method == "REDUCE" else "HANDLING"
            if self.recommendation_scope != expected or not self.evidence_ids or not self.source_urls:
                raise ValueError("READY membutuhkan scope, evidence dan sumber panduan yang sesuai.")
            if self.recommendation_basis not in {"CONFIRMED", "CONDITIONAL"}:
                raise ValueError("READY membutuhkan basis rekomendasi.")
            if (self.recommendation_basis == "CONDITIONAL") != bool(self.conditions_to_verify):
                raise ValueError("Saran bersyarat wajib menyebut hal yang perlu diperiksa.")
        elif self.recommendation_basis is not None or self.conditions_to_verify:
            raise ValueError("Fallback tidak memiliki basis/metode bersyarat.")
        return self
