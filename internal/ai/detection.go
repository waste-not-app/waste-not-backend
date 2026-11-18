package ai

func validDetection(v map[string]any) (valid bool) {
	defer func() {
		if recover() != nil {
			valid = false
		}
	}()
	model := v["model"].(map[string]any)
	if model["model_version"] != v["model_version"] {
		return false
	}
	labels := model["class_names"].([]any)
	objects := v["objects"].([]any)
	ids := map[string]map[string]any{}
	reviewable := false
	global := v["thresholds"].(map[string]any)
	if global["lower"].(float64) > global["offered"].(float64) {
		return false
	}
	dimensions := v["image"].(map[string]any)
	if dimensions["width"].(float64)*dimensions["height"].(float64) > 12000000 {
		return false
	}
	for _, entry := range objects {
		o := entry.(map[string]any)
		id := o["object_id"].(string)
		if ids[id] != nil {
			return false
		}
		ids[id] = o
		classNumber := o["class_id"].(float64)
		if classNumber < 0 || classNumber >= float64(len(labels)) {
			return false
		}
		classID := int(classNumber)
		if labels[classID] != o["raw_class"] {
			return false
		}
		box := o["bbox"].([]any)
		if box[0].(float64) >= box[2].(float64) || box[1].(float64) >= box[3].(float64) {
			return false
		}
		thresholds := v["thresholds"].(map[string]any)
		if t, ok := o["review_thresholds"].(map[string]any); ok {
			thresholds = t
		}
		lower, offered := thresholds["lower"].(float64), thresholds["offered"].(float64)
		if lower > offered {
			return false
		}
		supported := o["supported_for_review"].(bool)
		if supported != o["requires_material_confirmation"].(bool) {
			return false
		}
		band := "MANUAL_REQUIRED"
		keys := o["suggested_object_keys"].([]any)
		reasons, _ := o["review_reasons"].([]any)
		if supported {
			reviewable = true
			if len(keys) == 0 || o["confidence"].(float64) < lower {
				return false
			}
			band = "UNCERTAIN"
			if o["confidence"].(float64) >= offered && len(reasons) == 0 {
				band = "OFFERED"
			}
		} else if len(keys) > 0 || len(reasons) > 0 {
			return false
		}
		if o["confidence_band"] != band {
			return false
		}
	}
	for id, o := range ids {
		others, _ := o["ambiguous_with"].([]any)
		reasons, _ := o["review_reasons"].([]any)
		seen := map[any]bool{}
		overlap := false
		for _, reason := range reasons {
			if seen[reason] {
				return false
			}
			seen[reason] = true
			if reason == "OVERLAPPING_CLASSES" {
				overlap = true
			}
		}
		if overlap != (len(others) > 0) {
			return false
		}
		seen = map[any]bool{}
		for _, otherID := range others {
			if seen[otherID] {
				return false
			}
			seen[otherID] = true
			other := ids[otherID.(string)]
			if other == nil || otherID == id || other["class_id"] == o["class_id"] {
				return false
			}
			back, _ := other["ambiguous_with"].([]any)
			symmetric := false
			for _, backID := range back {
				if backID == id {
					symmetric = true
				}
			}
			if !symmetric {
				return false
			}
		}
	}
	return (v["status"] == "REVIEW_REQUIRED") == reviewable
}
