package redis

type scalarArgumentType string

const (
	scalarAny          scalarArgumentType = "any"
	scalarString       scalarArgumentType = "string"
	scalarInteger      scalarArgumentType = "integer"
	scalarNumber       scalarArgumentType = "number"
	scalarRedisBound   scalarArgumentType = "redis_bound"
	scalarBoolean      scalarArgumentType = "boolean"
	scalarNumberByKind scalarArgumentType = "number_by_kind"
	scalarByVariant    scalarArgumentType = "by_variant"
)

// scalarArgumentTypes is the operation-aware source of truth for scalar
// values that are not modeled by the structural contract fields. Both JSON
// Schema generation and pre-Adapter validation consume this metadata.
var scalarArgumentTypes = map[string]map[string]scalarArgumentType{
	"redis_key_write/copy":    {"replace": scalarBoolean, "destination_db": scalarInteger},
	"redis_key_write/delete":  {"async": scalarBoolean},
	"redis_key_write/rename":  {"only_if_absent": scalarBoolean},
	"redis_key_write/restore": {"ttl_ms": scalarInteger, "replace": scalarBoolean},

	"redis_string/get_range":       {"start": scalarInteger, "stop": scalarInteger},
	"redis_string_write/decrement": {"amount": scalarInteger},
	"redis_string_write/increment": {"amount": scalarNumberByKind},
	"redis_string_write/set":       {"return_previous": scalarBoolean},
	"redis_string_write/set_range": {"offset": scalarInteger},

	"redis_hash/random_field":    {"count": scalarInteger, "with_values": scalarBoolean},
	"redis_hash/scan":            {"count": scalarInteger, "no_values": scalarBoolean},
	"redis_hash_write/increment": {"amount": scalarNumberByKind},

	"redis_list/index":          {"index": scalarInteger},
	"redis_list/position":       {"rank": scalarInteger, "count": scalarInteger, "maxlen": scalarInteger},
	"redis_list/range":          {"start": scalarInteger, "count": scalarInteger},
	"redis_list_write/pop":      {"count": scalarInteger},
	"redis_list_write/pop_many": {"count": scalarInteger},
	"redis_list_write/remove":   {"count": scalarInteger},
	"redis_list_write/set":      {"index": scalarInteger},
	"redis_list_write/trim":     {"start": scalarInteger, "stop": scalarInteger},

	"redis_set/intersection_cardinality": {"limit": scalarInteger},
	"redis_set/random_members":           {"count": scalarInteger},
	"redis_set/scan":                     {"count": scalarInteger},
	"redis_set_write/pop":                {"count": scalarInteger},

	"redis_sorted_set/count_by_score":           {"min": scalarRedisBound, "max": scalarRedisBound},
	"redis_sorted_set/intersection_cardinality": {"limit": scalarInteger},
	"redis_sorted_set/lex_count":                {"min": scalarString, "max": scalarString},
	"redis_sorted_set/random_members":           {"count": scalarInteger, "with_scores": scalarBoolean},
	"redis_sorted_set/range":                    {"count": scalarInteger, "reverse": scalarBoolean, "offset": scalarInteger, "with_scores": scalarBoolean},
	"redis_sorted_set/rank":                     {"reverse": scalarBoolean, "with_score": scalarBoolean},
	"redis_sorted_set/scan":                     {"count": scalarInteger},
	"redis_sorted_set_write/add":                {"change": scalarBoolean, "increment": scalarBoolean},
	"redis_sorted_set_write/increment":          {"increment": scalarNumber},
	"redis_sorted_set_write/pop":                {"count": scalarInteger},
	"redis_sorted_set_write/store_range":        {"reverse": scalarBoolean, "offset": scalarInteger, "count": scalarInteger},

	"redis_stream/pending":     {"count": scalarInteger},
	"redis_stream_write/group": {"mkstream": scalarBoolean},
	"redis_stream_write/add":   {"nomkstream": scalarBoolean, "maxlen": scalarInteger, "approximate": scalarBoolean, "limit": scalarInteger},
	"redis_stream_write/claim": {"min_idle_ms": scalarInteger, "justid": scalarBoolean},
	"redis_stream_write/nack":  {"retry_count": scalarInteger, "force": scalarBoolean},
	"redis_stream_write/trim":  {"threshold": scalarAny, "approximate": scalarBoolean, "limit": scalarInteger},

	"redis_observe/scan":         {"count": scalarInteger},
	"redis_observe/memory_usage": {"samples": scalarInteger},
}

// variantScalarArgumentTypes describes argument types selected by an
// operation's enum variant. Schema generation and pre-Adapter validation use
// this table together so the variant cannot drift into a coercing adapter.
var variantScalarArgumentTypes = map[string]map[string]map[string]scalarArgumentType{
	"redis_sorted_set/range": {
		"start": {"rank": scalarInteger},
		"min":   {"lex": scalarString, "score": scalarRedisBound},
		"max":   {"lex": scalarString, "score": scalarRedisBound},
	},
	"redis_sorted_set_write/remove_range": {
		"min": {"rank": scalarInteger, "lex": scalarString, "score": scalarRedisBound},
		"max": {"rank": scalarInteger, "lex": scalarString, "score": scalarRedisBound},
	},
	"redis_sorted_set_write/store_range": {
		"min": {"rank": scalarInteger, "lex": scalarString, "score": scalarRedisBound},
		"max": {"rank": scalarInteger, "lex": scalarString, "score": scalarRedisBound},
	},
}

func scalarTypeFor(tool, operation, name string) scalarArgumentType {
	if arguments, ok := variantScalarArgumentTypes[tool+"/"+operation]; ok {
		if _, ok := arguments[name]; ok {
			return scalarByVariant
		}
	}
	if types, ok := scalarArgumentTypes[tool+"/"+operation]; ok {
		if argumentType, ok := types[name]; ok {
			return argumentType
		}
	}
	return scalarString
}

func scalarTypeForArgs(tool, operation, name string, args map[string]any) scalarArgumentType {
	if arguments, ok := variantScalarArgumentTypes[tool+"/"+operation]; ok {
		if variants, ok := arguments[name]; ok {
			by, _ := args["by"].(string)
			if argumentType, ok := variants[by]; ok {
				return argumentType
			}
			return scalarByVariant
		}
	}
	return scalarTypeFor(tool, operation, name)
}
