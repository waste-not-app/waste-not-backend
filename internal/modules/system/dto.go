package system

type HealthLiveRes struct {
	Live bool `json:"live"`
}

type HealthReadyRes struct {
	Ready bool `json:"ready"`
}

type RangeLimit struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type LimitsRes struct {
	WeightG              RangeLimit `json:"weight_g"`
	ItemCount            RangeLimit `json:"item_count"`
	NameLength           RangeLimit `json:"name_length"`
	EmailMaxLength       int        `json:"email_max_length"`
	PasswordLength       RangeLimit `json:"password_length"`
	NotesMaxLength       int        `json:"notes_max_length"`
	ActivityDatePastDays int        `json:"activity_date_past_days"`
	KgMaxDecimalPlaces   int        `json:"kg_max_decimal_places"`
}

type CapabilitiesRes struct {
	Achievements     bool `json:"achievements"`
	Reminders        bool `json:"reminders"`
	Photos           bool `json:"photos"`
	RecyclingPoints  bool `json:"recycling_points"`
	AIScan           bool `json:"ai_scan"`
	AIRecommendation bool `json:"ai_recommendation"`
}

type MetadataRes struct {
	Categories             []string        `json:"categories"`
	Methods                []string        `json:"methods"`
	Limits                 LimitsRes       `json:"limits"`
	Capabilities           CapabilitiesRes `json:"capabilities"`
	AIObjectCatalogVersion any             `json:"ai_object_catalog_version" swaggertype:"string"`
}
