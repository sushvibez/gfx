package gml

// null and undefined.
//
// JS has two "nothing" values and code tells them apart: `x === undefined`
// asks "was it given", `x === null` asks "was it given as nothing", and a
// null argument coerces to 0 where an undefined one means "use the default"
// ("abc".slice(0, null) is "", "abc".slice(0, undefined) is "abc").
//
// Go's nil is UNDEFINED. That is what nil already meant everywhere a value
// is absent: a missing key, an index out of range, an argument not passed,
// a function that returns nothing, a `property var` with no initializer,
// a host value that is nil. None of those sites had to change.
//
// null is the one new value, Null, and only three things make it: the
// literal `null`, JSON.parse, and a host that passes Null on purpose. It is
// falsy, 0 as a number, the empty string as text (as nil is — see str),
// loosely equal to undefined and strictly equal only to itself.
//
// A host does not have to know any of this: what it reads back
// (Document.Property, RefValue) and what its functions are called with is
// nil for null, as it was before the two were separate.

// nullValue is the type of Null. It is a zero-size struct so that a test
// for it is one type-word comparison and holding it allocates nothing.
type nullValue struct{}

// Null is JS's null as a value a host can pass in (Env.Set, a model row,
// a function result). Go's nil is undefined.
var Null any = nullValue{}

// isNull reports whether v is null (and not undefined).
func isNull(v any) bool {
	_, ok := v.(nullValue)
	return ok
}

// nullish reports whether v is null or undefined — what ==, ?? and ?. ask.
func nullish(v any) bool {
	if v == nil {
		return true
	}
	_, ok := v.(nullValue)
	return ok
}

// hostValue is v as a host reads it: nil for null.
func hostValue(v any) any {
	if isNull(v) {
		return nil
	}
	return v
}

// hostArgs is a call's arguments as a host function receives them: nil for
// null. The slice is copied only when there is a null in it.
func hostArgs(args []any) []any {
	for i, a := range args {
		if isNull(a) {
			out := append([]any(nil), args...)
			for j := i; j < len(out); j++ {
				out[j] = hostValue(out[j])
			}
			return out
		}
	}
	return args
}
