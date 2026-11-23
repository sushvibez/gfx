package gml

import (
	"fmt"
	"reflect"
)

// adaptHostFunc wraps a Go function of any other shape whose parameters are
// strings, bools, numbers (any int, uint or float kind) or `any`, returning
// nothing, a value, an error, or (value, error), as a func([]any) (any,
// error). Markup values convert to each parameter as JS would (a number for
// a string parameter formats, a string for a number parses, null is the
// zero value); a call with the wrong number of arguments is an error. ok is
// false for a shape it cannot adapt, so Env.Func still rejects those at
// registration.
func adaptHostFunc(fn any) (_ func([]any) (any, error), ok bool) {
	v := reflect.ValueOf(fn)
	t := v.Type()
	if t.Kind() != reflect.Func || t.IsVariadic() || v.IsNil() {
		return nil, false
	}
	conv := make([]func(any) (reflect.Value, error), t.NumIn())
	for i := range conv {
		if conv[i] = argConverter(t.In(i)); conv[i] == nil {
			return nil, false
		}
	}
	errAt := -1
	switch t.NumOut() {
	case 0:
	case 1:
		if t.Out(0) == errorType {
			errAt = 0
		}
	case 2:
		if t.Out(1) != errorType {
			return nil, false
		}
		errAt = 1
	default:
		return nil, false
	}
	return func(args []any) (any, error) {
		if len(args) != len(conv) {
			return nil, fmt.Errorf("takes %d arguments, got %d", len(conv), len(args))
		}
		in := make([]reflect.Value, len(args))
		for i, a := range args {
			pv, err := conv[i](a)
			if err != nil {
				return nil, fmt.Errorf("argument %d: %w", i+1, err)
			}
			in[i] = pv
		}
		out := v.Call(in)
		if errAt >= 0 && !out[errAt].IsNil() {
			return nil, out[errAt].Interface().(error)
		}
		if len(out) > 0 && errAt != 0 {
			return out[0].Interface(), nil
		}
		return nil, nil
	}, true
}

var errorType = reflect.TypeFor[error]()

// argConverter returns the markup-value-to-parameter conversion for t, or
// nil when t is not a supported parameter type.
func argConverter(t reflect.Type) func(any) (reflect.Value, error) {
	switch t.Kind() {
	case reflect.String:
		return func(a any) (reflect.Value, error) {
			if a == nil {
				return reflect.Zero(t), nil
			}
			return reflect.ValueOf(str(a)).Convert(t), nil
		}
	case reflect.Bool:
		return func(a any) (reflect.Value, error) {
			b, err := toBool(a)
			return reflect.ValueOf(b).Convert(t), err
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return func(a any) (reflect.Value, error) {
			if a == nil {
				return reflect.Zero(t), nil
			}
			f, err := toFloat64(a)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(f).Convert(t), nil
		}
	case reflect.Interface:
		if t.NumMethod() != 0 {
			return nil
		}
		return func(a any) (reflect.Value, error) {
			if a == nil {
				return reflect.Zero(t), nil
			}
			return reflect.ValueOf(a), nil
		}
	}
	return nil
}
