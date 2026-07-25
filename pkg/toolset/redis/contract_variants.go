package redis

import "fmt"

type variantValidator func(tool, operation string, args map[string]any) error

var variantValidators = []variantValidator{
	validateListRangeVariant,
	validateStreamPendingVariant,
	validateConditionalSetVariant,
	validateSortedSetAddVariant,
	validateStreamTrimVariant,
	validateStreamAddVariant,
	validateSortedSetRangeVariant,
	validateKeyExpiryVariant,
	validateStreamGroupVariant,
	validateStreamClaimVariant,
}

func validateListRangeVariant(tool, operation string, args map[string]any) error {
	if tool != "redis_list" || operation != "range" {
		return nil
	}
	start, ok := integer(args["start"])
	if !ok || start < 0 {
		return fmt.Errorf("argument start must be a non-negative integer for list range")
	}
	return nil
}

func validateStreamPendingVariant(tool, operation string, args map[string]any) error {
	if tool == "redis_stream" && operation == "pending" {
		if _, ok := args["start"].(string); !ok {
			return fmt.Errorf("argument start must be a stream ID string")
		}
	}
	return nil
}

func validateConditionalSetVariant(tool, operation string, args map[string]any) error {
	if tool != "redis_string_write" || operation != "set" {
		return nil
	}
	condition, _ := args["condition"].(string)
	returnPrevious, _ := args["return_previous"].(bool)
	if returnPrevious && (condition == "ifne" || condition == "ifdne") {
		return fmt.Errorf("argument return_previous is unavailable for condition %q", condition)
	}
	_, hasComparison := args["comparison"]
	comparisonCondition := condition == "ifeq" || condition == "ifne" || condition == "ifdeq" || condition == "ifdne"
	if comparisonCondition && !hasComparison {
		return fmt.Errorf("argument comparison is required for condition %q", condition)
	}
	if hasComparison && !comparisonCondition {
		return fmt.Errorf("argument comparison requires an equality conditional SET mode")
	}
	return nil
}

func validateSortedSetAddVariant(tool, operation string, args map[string]any) error {
	if tool == "redis_sorted_set_write" && operation == "add" && args["increment"] == true {
		items, _ := args["items"].([]any)
		if len(items) != 1 {
			return fmt.Errorf("argument items must contain exactly one item when increment is true")
		}
	}
	return nil
}

func validateStreamTrimVariant(tool, operation string, args map[string]any) error {
	if tool != "redis_stream_write" || operation != "trim" {
		return nil
	}
	if args["strategy"] == "maxlen" {
		threshold, ok := integer(args["threshold"])
		if !ok || threshold < 0 {
			return fmt.Errorf("argument threshold must be a non-negative integer for maxlen")
		}
	} else if _, ok := args["threshold"].(string); !ok {
		return fmt.Errorf("argument threshold must be a stream ID string for minid")
	}
	if _, hasLimit := args["limit"]; hasLimit && args["approximate"] != true {
		return fmt.Errorf("argument limit requires approximate stream trimming")
	}
	return nil
}

func validateStreamAddVariant(tool, operation string, args map[string]any) error {
	if tool != "redis_stream_write" || operation != "add" {
		return nil
	}
	_, hasMaxLen := args["maxlen"]
	_, hasMinID := args["minid"]
	if hasMaxLen == hasMinID {
		return fmt.Errorf("exactly one of maxlen or minid is required for stream add")
	}
	if _, hasLimit := args["limit"]; hasLimit && args["approximate"] != true {
		return fmt.Errorf("argument limit requires approximate stream trimming")
	}
	return nil
}

func validateSortedSetRangeVariant(tool, operation string, args map[string]any) error {
	if tool != "redis_sorted_set" || operation != "range" {
		return nil
	}
	if args["by"] == "rank" {
		start, ok := integer(args["start"])
		if !ok || start < 0 {
			return fmt.Errorf("argument start must be a non-negative integer for rank range")
		}
		if _, ok := args["min"]; ok {
			return fmt.Errorf("argument min is not supported for rank range")
		}
		if _, ok := args["max"]; ok {
			return fmt.Errorf("argument max is not supported for rank range")
		}
		if _, ok := args["offset"]; ok {
			return fmt.Errorf("argument offset is not supported for rank range")
		}
	} else {
		if _, ok := args["min"]; !ok {
			return fmt.Errorf("argument min is required for %s range", args["by"])
		}
		if _, ok := args["max"]; !ok {
			return fmt.Errorf("argument max is required for %s range", args["by"])
		}
		if _, ok := args["start"]; ok {
			return fmt.Errorf("argument start is only supported for rank range")
		}
	}
	if offset, present := args["offset"]; present {
		value, ok := integer(offset)
		if !ok || value < 0 {
			return fmt.Errorf("argument offset must be a non-negative integer")
		}
	}
	return nil
}

func validateKeyExpiryVariant(tool, operation string, args map[string]any) error {
	if tool == "redis_key_write" && operation == "set_expiry" {
		if _, hasCondition := args["condition"]; hasCondition {
			expiry := args["expiry"].(map[string]any)
			if expiry["kind"] != "ex" {
				return fmt.Errorf("expiry conditions are only supported with ex")
			}
		}
	}
	return nil
}

func validateStreamGroupVariant(tool, operation string, args map[string]any) error {
	if tool != "redis_stream_write" || operation != "group" {
		return nil
	}
	action := args["action"].(string)
	required := map[string][]string{"create": {"id"}, "create_consumer": {"consumer"}, "delete_consumer": {"consumer"}, "destroy": nil, "set_id": {"id"}}[action]
	for _, name := range required {
		if _, ok := args[name]; !ok {
			return fmt.Errorf("argument %q is required for group action %q", name, action)
		}
	}
	allowed := map[string][]string{
		"create":          {"action", "key", "group", "id", "mkstream"},
		"create_consumer": {"action", "key", "group", "consumer"},
		"delete_consumer": {"action", "key", "group", "consumer"},
		"destroy":         {"action", "key", "group"},
		"set_id":          {"action", "key", "group", "id"},
	}[action]
	for name := range args {
		if !contains(allowed, name) {
			return fmt.Errorf("argument %q is not allowed for group action %q", name, action)
		}
	}
	return nil
}

func validateStreamClaimVariant(tool, operation string, args map[string]any) error {
	if tool == "redis_stream_write" && operation == "claim" && args["justid"] != true {
		return fmt.Errorf("argument justid must be true for stream claim")
	}
	return nil
}
