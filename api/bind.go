package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

type Binder interface {
	Bind(value string) error
}

var errInvalidType = errors.New("invalid type")

func Bind(r *http.Request, dest interface{}) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	val := reflect.ValueOf(dest)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("%w: %T", errInvalidType, dest)
	}

	val = val.Elem()
	for i := 0; i < val.NumField(); i++ {
		fieldType := val.Type().Field(i)

		tag := fieldType.Tag
		formField := tag.Get("form")
		if formField == "" {
			formField = strings.ToLower(fieldType.Name)
		}

		fieldValue := val.Field(i)

		// If the field value is a pointer, we unwrap it.
		if fieldValue.Kind() == reflect.Ptr {
			if fieldValue.IsNil() {
				// Allocate a value for the pointer.
				fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
			}
			fieldValue = fieldValue.Elem()
		}

		formValue := r.FormValue(formField)
		if fieldValue.CanAddr() && fieldValue.Addr().Type().Implements(reflect.TypeOf((*Binder)(nil)).Elem()) {
			if err := fieldValue.Addr().Interface().(Binder).Bind(formValue); err != nil {
				return err
			}
			continue
		}

		if formValue == "" {
			continue
		}

		switch fieldValue.Kind() {
		case reflect.String:
			fieldValue.SetString(formValue)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v, err := strconv.ParseInt(formValue, 10, 64)
			if err != nil {
				return err
			}
			fieldValue.SetInt(v)
		case reflect.Float32, reflect.Float64:
			v, err := strconv.ParseFloat(formValue, 64)
			if err != nil {
				return err
			}
			fieldValue.SetFloat(v)
		case reflect.Bool:
			v, err := strconv.ParseBool(formValue)
			if err != nil {
				return err
			}
			fieldValue.SetBool(v)
		default:
			return fmt.Errorf("%w: %s", errInvalidType, fieldType.Name)
		}
	}

	if r.Header.Get("Content-Type") == "application/json" {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(dest); err != nil {
			return err
		}
	}

	return nil
}
