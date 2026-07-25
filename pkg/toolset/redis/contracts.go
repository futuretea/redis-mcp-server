package redis

// operationContract is the single source of truth for the closed Redis MCP
// surface. The handler enforces it and the schema is derived from it.
type operationContract struct {
	required    []string
	optional    []string
	enums       map[string][]string
	binary      []string
	binaryLists []string
	arrays      map[string]string
	sameSlot    bool
}

func contract(required, optional []string, sameSlot bool, enums map[string][]string, binary, binaryLists []string, arrays map[string]string) operationContract {
	return operationContract{required: required, optional: optional, sameSlot: sameSlot, enums: enums, binary: binary, binaryLists: binaryLists, arrays: arrays}
}

var contracts = map[string]operationContract{
	"redis_key/exists":           contract([]string{"keys"}, nil, true, nil, nil, nil, map[string]string{"keys": "string"}),
	"redis_key/random_key":       contract(nil, nil, false, nil, nil, nil, nil),
	"redis_key/type":             contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_key/expiry":           contract([]string{"key", "unit", "representation"}, nil, false, map[string][]string{"unit": {"seconds", "milliseconds"}, "representation": {"relative", "absolute"}}, nil, nil, nil),
	"redis_key/dump":             contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_key/object_attribute": contract([]string{"key", "attribute"}, nil, false, map[string][]string{"attribute": {"encoding", "freq", "idletime", "refcount"}}, nil, nil, nil),

	"redis_key_write/copy":           contract([]string{"source_key", "destination_key"}, []string{"replace", "destination_db"}, true, nil, nil, nil, nil),
	"redis_key_write/delete":         contract([]string{"keys"}, []string{"async"}, true, nil, nil, nil, map[string]string{"keys": "string"}),
	"redis_key_write/set_expiry":     contract([]string{"key", "expiry"}, []string{"condition"}, false, map[string][]string{"condition": {"nx", "xx", "gt", "lt"}}, nil, nil, nil),
	"redis_key_write/persist_expiry": contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_key_write/rename":         contract([]string{"source_key", "destination_key"}, []string{"only_if_absent"}, true, nil, nil, nil, nil),
	"redis_key_write/restore":        contract([]string{"key", "ttl_ms", "payload"}, []string{"replace"}, false, nil, []string{"payload"}, nil, nil),
	"redis_key_write/touch":          contract([]string{"keys"}, nil, true, nil, nil, nil, map[string]string{"keys": "string"}),

	"redis_string/get":                      contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_string/get_range":                contract([]string{"key", "start", "stop"}, nil, false, nil, nil, nil, nil),
	"redis_string/get_many":                 contract([]string{"keys"}, nil, true, nil, nil, nil, map[string]string{"keys": "string"}),
	"redis_string/length":                   contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_string/digest":                   contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_string/longest_common_substring": contract([]string{"first_key", "second_key"}, []string{"mode"}, true, map[string][]string{"mode": {"value", "length"}}, nil, nil, nil),
	"redis_string_write/append":             contract([]string{"key", "value"}, nil, false, nil, []string{"value"}, nil, nil),
	"redis_string_write/decrement":          contract([]string{"key"}, []string{"amount"}, false, nil, nil, nil, nil),
	"redis_string_write/delete_if":          contract([]string{"key", "condition", "comparison"}, nil, false, map[string][]string{"condition": {"ifeq", "ifne", "ifdeq", "ifdne"}}, []string{"comparison"}, nil, nil),
	"redis_string_write/get_and_delete":     contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_string_write/get_and_set_expiry": contract([]string{"key", "expiry"}, nil, false, nil, nil, nil, nil),
	"redis_string_write/get_and_set":        contract([]string{"key", "value"}, nil, false, nil, []string{"value"}, nil, nil),
	"redis_string_write/increment":          contract([]string{"key"}, []string{"amount", "number_kind", "expiry"}, false, map[string][]string{"number_kind": {"integer", "float"}}, nil, nil, nil),
	"redis_string_write/set_many":           contract([]string{"items"}, []string{"condition", "expiry"}, true, map[string][]string{"condition": {"nx", "xx"}}, nil, nil, map[string]string{"items": "key_value"}),
	"redis_string_write/set":                contract([]string{"key", "value"}, []string{"condition", "comparison", "return_previous", "expiry"}, false, map[string][]string{"condition": {"nx", "xx", "ifeq", "ifne", "ifdeq", "ifdne"}}, []string{"value", "comparison"}, nil, nil),
	"redis_string_write/set_range":          contract([]string{"key", "offset", "value"}, nil, false, nil, []string{"value"}, nil, nil),

	"redis_hash/field_exists": contract([]string{"key", "field"}, nil, false, nil, []string{"field"}, nil, nil),
	"redis_hash/get":          contract([]string{"key", "field"}, nil, false, nil, []string{"field"}, nil, nil),
	"redis_hash/length":       contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_hash/get_many":     contract([]string{"key", "fields"}, nil, false, nil, nil, []string{"fields"}, map[string]string{"fields": "binary"}),
	"redis_hash/field_expiry": contract([]string{"key", "fields", "unit", "representation"}, nil, false, map[string][]string{"unit": {"seconds", "milliseconds"}, "representation": {"relative", "absolute"}}, nil, []string{"fields"}, map[string]string{"fields": "binary"}),
	"redis_hash/random_field": contract([]string{"key"}, []string{"count", "with_values"}, false, nil, nil, nil, nil),
	"redis_hash/scan":         contract([]string{"key", "count"}, []string{"cursor", "match", "no_values"}, false, nil, nil, nil, nil),
	"redis_hash/value_length": contract([]string{"key", "field"}, nil, false, nil, []string{"field"}, nil, nil),

	"redis_hash_write/delete_fields":          contract([]string{"key", "fields"}, nil, false, nil, nil, []string{"fields"}, map[string]string{"fields": "binary"}),
	"redis_hash_write/set_field_expiry":       contract([]string{"key", "fields", "expiry"}, []string{"condition"}, false, map[string][]string{"condition": {"nx", "xx", "gt", "lt"}}, nil, []string{"fields"}, map[string]string{"fields": "binary"}),
	"redis_hash_write/persist_field_expiry":   contract([]string{"key", "fields"}, nil, false, nil, nil, []string{"fields"}, map[string]string{"fields": "binary"}),
	"redis_hash_write/get_and_delete":         contract([]string{"key", "fields"}, nil, false, nil, nil, []string{"fields"}, map[string]string{"fields": "binary"}),
	"redis_hash_write/get_and_set_expiry":     contract([]string{"key", "fields", "expiry"}, nil, false, nil, nil, []string{"fields"}, map[string]string{"fields": "binary"}),
	"redis_hash_write/increment":              contract([]string{"key", "field", "amount"}, []string{"number_kind"}, false, map[string][]string{"number_kind": {"integer", "float"}}, []string{"field"}, nil, nil),
	"redis_hash_write/set_fields":             contract([]string{"key", "fields"}, nil, false, nil, nil, nil, map[string]string{"fields": "field_value"}),
	"redis_hash_write/set_fields_with_expiry": contract([]string{"key", "fields"}, []string{"condition", "expiry"}, false, map[string][]string{"condition": {"fnx", "fxx"}}, nil, nil, map[string]string{"fields": "field_value"}),
	"redis_hash_write/set_if_absent":          contract([]string{"key", "field", "value"}, nil, false, nil, []string{"field", "value"}, nil, nil),

	"redis_list/index":                contract([]string{"key", "index"}, nil, false, nil, nil, nil, nil),
	"redis_list/length":               contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_list/position":             contract([]string{"key", "element"}, []string{"rank", "count", "maxlen"}, false, nil, []string{"element"}, nil, nil),
	"redis_list/range":                contract([]string{"key", "start", "count"}, nil, false, nil, nil, nil, nil),
	"redis_list_write/insert":         contract([]string{"key", "where", "pivot", "element"}, nil, false, map[string][]string{"where": {"before", "after"}}, []string{"pivot", "element"}, nil, nil),
	"redis_list_write/move":           contract([]string{"source_key", "destination_key", "from", "to"}, nil, true, map[string][]string{"from": {"left", "right"}, "to": {"left", "right"}}, nil, nil, nil),
	"redis_list_write/pop":            contract([]string{"key", "side"}, []string{"count"}, false, map[string][]string{"side": {"left", "right"}}, nil, nil, nil),
	"redis_list_write/pop_many":       contract([]string{"keys", "side"}, []string{"count"}, true, map[string][]string{"side": {"left", "right"}}, nil, nil, map[string]string{"keys": "string"}),
	"redis_list_write/push":           contract([]string{"key", "side", "elements"}, nil, false, map[string][]string{"side": {"left", "right"}}, nil, []string{"elements"}, map[string]string{"elements": "binary"}),
	"redis_list_write/push_if_exists": contract([]string{"key", "side", "elements"}, nil, false, map[string][]string{"side": {"left", "right"}}, nil, []string{"elements"}, map[string]string{"elements": "binary"}),
	"redis_list_write/remove":         contract([]string{"key", "count", "element"}, nil, false, nil, []string{"element"}, nil, nil),
	"redis_list_write/set":            contract([]string{"key", "index", "element"}, nil, false, nil, []string{"element"}, nil, nil),
	"redis_list_write/trim":           contract([]string{"key", "start", "stop"}, nil, false, nil, nil, nil, nil),

	"redis_set/cardinality":              contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_set/intersection_cardinality": contract([]string{"keys"}, []string{"limit"}, true, nil, nil, nil, map[string]string{"keys": "string"}),
	"redis_set/member_exists":            contract([]string{"key", "member"}, nil, false, nil, []string{"member"}, nil, nil),
	"redis_set/members_exist":            contract([]string{"key", "members"}, nil, false, nil, nil, []string{"members"}, map[string]string{"members": "binary"}),
	"redis_set/random_members":           contract([]string{"key"}, []string{"count"}, false, nil, nil, nil, nil),
	"redis_set/scan":                     contract([]string{"key", "count"}, []string{"cursor", "match"}, false, nil, nil, nil, nil),
	"redis_set_write/add":                contract([]string{"key", "members"}, nil, false, nil, nil, []string{"members"}, map[string]string{"members": "binary"}),
	"redis_set_write/store_difference":   contract([]string{"destination_key", "keys"}, nil, true, nil, nil, nil, map[string]string{"keys": "string"}),
	"redis_set_write/store_intersection": contract([]string{"destination_key", "keys"}, nil, true, nil, nil, nil, map[string]string{"keys": "string"}),
	"redis_set_write/move":               contract([]string{"source_key", "destination_key", "member"}, nil, true, nil, []string{"member"}, nil, nil),
	"redis_set_write/pop":                contract([]string{"key"}, []string{"count"}, false, nil, nil, nil, nil),
	"redis_set_write/remove":             contract([]string{"key", "members"}, nil, false, nil, nil, []string{"members"}, map[string]string{"members": "binary"}),
	"redis_set_write/store_union":        contract([]string{"destination_key", "keys"}, nil, true, nil, nil, nil, map[string]string{"keys": "string"}),

	"redis_sorted_set/cardinality":              contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_sorted_set/count_by_score":           contract([]string{"key", "min", "max"}, nil, false, nil, nil, nil, nil),
	"redis_sorted_set/intersection_cardinality": contract([]string{"keys"}, []string{"limit"}, true, nil, nil, nil, map[string]string{"keys": "string"}),
	"redis_sorted_set/lex_count":                contract([]string{"key", "min", "max"}, nil, false, nil, nil, nil, nil),
	"redis_sorted_set/scores":                   contract([]string{"key", "members"}, nil, false, nil, nil, []string{"members"}, map[string]string{"members": "binary"}),
	"redis_sorted_set/random_members":           contract([]string{"key"}, []string{"count", "with_scores"}, false, nil, nil, nil, nil),
	"redis_sorted_set/range":                    contract([]string{"key", "by"}, []string{"start", "min", "max", "count", "reverse", "offset", "with_scores"}, false, map[string][]string{"by": {"rank", "score", "lex"}}, nil, nil, nil),
	"redis_sorted_set/rank":                     contract([]string{"key", "member"}, []string{"reverse", "with_score"}, false, nil, []string{"member"}, nil, nil),
	"redis_sorted_set/scan":                     contract([]string{"key", "count"}, []string{"cursor", "match"}, false, nil, nil, nil, nil),
	"redis_sorted_set/score":                    contract([]string{"key", "member"}, nil, false, nil, []string{"member"}, nil, nil),
	"redis_sorted_set_write/add":                contract([]string{"key", "items"}, []string{"condition", "change", "increment"}, false, map[string][]string{"condition": {"nx", "xx", "gt", "lt"}}, nil, nil, map[string]string{"items": "score_member"}),
	"redis_sorted_set_write/store_difference":   contract([]string{"destination_key", "keys"}, nil, true, nil, nil, nil, map[string]string{"keys": "string"}),
	"redis_sorted_set_write/increment":          contract([]string{"key", "increment", "member"}, nil, false, nil, []string{"member"}, nil, nil),
	"redis_sorted_set_write/store_intersection": contract([]string{"destination_key", "keys"}, []string{"weights", "aggregate"}, true, map[string][]string{"aggregate": {"sum", "min", "max"}}, nil, nil, map[string]string{"keys": "string", "weights": "number"}),
	"redis_sorted_set_write/pop":                contract([]string{"keys", "side"}, []string{"count"}, true, map[string][]string{"side": {"min", "max"}}, nil, nil, map[string]string{"keys": "string"}),
	"redis_sorted_set_write/remove":             contract([]string{"key", "members"}, nil, false, nil, nil, []string{"members"}, map[string]string{"members": "binary"}),
	"redis_sorted_set_write/remove_range":       contract([]string{"key", "by", "min", "max"}, nil, false, map[string][]string{"by": {"lex", "rank", "score"}}, nil, nil, nil),
	"redis_sorted_set_write/store_range":        contract([]string{"destination_key", "source_key", "by", "min", "max"}, []string{"reverse", "offset", "count"}, true, map[string][]string{"by": {"rank", "score", "lex"}}, nil, nil, nil),
	"redis_sorted_set_write/store_union":        contract([]string{"destination_key", "keys"}, []string{"weights", "aggregate"}, true, map[string][]string{"aggregate": {"sum", "min", "max"}}, nil, nil, map[string]string{"keys": "string", "weights": "number"}),

	"redis_stream/length":  contract([]string{"key"}, nil, false, nil, nil, nil, nil),
	"redis_stream/pending": contract([]string{"key", "group", "start", "end", "count"}, []string{"consumer"}, false, nil, nil, nil, nil),

	"redis_stream_write/group":  contract([]string{"action", "key", "group"}, []string{"consumer", "id", "mkstream"}, false, map[string][]string{"action": {"create", "create_consumer", "delete_consumer", "destroy", "set_id"}}, nil, nil, nil),
	"redis_stream_write/ack":    contract([]string{"key", "group", "ids"}, []string{"delete_references"}, false, map[string][]string{"delete_references": {"keepref", "delref", "acked"}}, nil, nil, map[string]string{"ids": "string"}),
	"redis_stream_write/add":    contract([]string{"key", "fields"}, []string{"id", "nomkstream", "maxlen", "minid", "approximate", "limit"}, false, nil, nil, nil, map[string]string{"fields": "field_value"}),
	"redis_stream_write/claim":  contract([]string{"key", "group", "consumer", "min_idle_ms", "ids", "justid"}, nil, false, nil, nil, nil, map[string]string{"ids": "string"}),
	"redis_stream_write/delete": contract([]string{"key", "ids"}, []string{"delete_references"}, false, map[string][]string{"delete_references": {"keepref", "delref", "acked"}}, nil, nil, map[string]string{"ids": "string"}),
	"redis_stream_write/nack":   contract([]string{"key", "group", "mode", "ids"}, []string{"retry_count", "force"}, false, map[string][]string{"mode": {"silent", "fail", "fatal"}}, nil, nil, map[string]string{"ids": "string"}),
	"redis_stream_write/trim":   contract([]string{"key", "strategy", "threshold"}, []string{"approximate", "limit"}, false, map[string][]string{"strategy": {"maxlen", "minid"}}, nil, nil, nil),

	"redis_observe/scan":         contract([]string{"count"}, []string{"cursor", "match", "type"}, false, nil, nil, nil, nil),
	"redis_observe/db_size":      contract(nil, nil, false, nil, nil, nil, nil),
	"redis_observe/memory_usage": contract([]string{"key"}, []string{"samples"}, false, nil, nil, nil, nil),
}
