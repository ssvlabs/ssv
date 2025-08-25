package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CSV represents a comma‐separated list.
type CSV []string

// Bind implements binding for CSV by splitting the input string.
func (c *CSV) Bind(value string) error {
	*c = strings.Split(value, ",")
	return nil
}

// TestStructNonPointer is a test struct with non‐pointer field types.
type TestStructNonPointer struct {
	Name  string `form:"name"`
	Age   int    `form:"age"`
	Email string `form:"email"`
	Tags  CSV    `form:"tags"`
}

// TestStructPointer is a test struct with a pointer field for tags.
type TestStructPointer struct {
	Name  string `form:"name"`
	Age   int    `form:"age"`
	Email string `form:"email"`
	Tags  *CSV   `form:"tags"`
}

// runTestBindForm is a helper that binds form data to dest and then runs the provided validation.
func runTestBindForm(t *testing.T, dest interface{}, validate func(*testing.T, interface{})) {
	form := url.Values{
		"name":  []string{"John Doe"},
		"age":   []string{"30"},
		"email": []string{"john.doe@example.com"},
		"tags":  []string{"tag1,tag2,tag3"},
	}
	req, err := http.NewRequest("POST", "", strings.NewReader(form.Encode()))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	require.NoError(t, Bind(req, dest))

	validate(t, dest)
}

// TestBindFormNonPointer verifies binding of form data to a struct with non‐pointer fields.
func TestBindFormNonPointer(t *testing.T) {
	t.Parallel()

	dest := &TestStructNonPointer{}
	validate := func(t *testing.T, dest interface{}) {
		s, ok := dest.(*TestStructNonPointer)

		require.True(t, ok)

		assert.Equal(t, "John Doe", s.Name)
		assert.Equal(t, 30, s.Age)
		assert.Equal(t, "john.doe@example.com", s.Email)
		assert.Equal(t, CSV{"tag1", "tag2", "tag3"}, s.Tags)
	}

	runTestBindForm(t, dest, validate)
}

// TestBindFormPointer verifies binding of form data to a struct with a pointer field.
func TestBindFormPointer(t *testing.T) {
	t.Parallel()

	dest := &TestStructPointer{}
	validate := func(t *testing.T, dest interface{}) {
		s, ok := dest.(*TestStructPointer)

		require.True(t, ok)

		assert.Equal(t, "John Doe", s.Name)
		assert.Equal(t, 30, s.Age)
		assert.Equal(t, "john.doe@example.com", s.Email)
		assert.Equal(t, &CSV{"tag1", "tag2", "tag3"}, s.Tags)
	}

	runTestBindForm(t, dest, validate)
}

// TestBindJSON verifies that JSON binding populates struct fields correctly.
func TestBindJSON(t *testing.T) {
	t.Parallel()

	jsonData := `{"name":"Jane Smith","age":25,"email":"jane@example.com"}`
	req, err := http.NewRequest("POST", "", strings.NewReader(jsonData))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	dest := &TestStructNonPointer{}

	require.NoError(t, Bind(req, dest))

	assert.Equal(t, "Jane Smith", dest.Name)
	assert.Equal(t, 25, dest.Age)
	assert.Equal(t, "jane@example.com", dest.Email)
}

// TestBindJSONError verifies that invalid JSON data results in a binding error.
func TestBindJSONError(t *testing.T) {
	t.Parallel()

	invalidJSON := `{"name":"Jane Smith","age":"invalid"}`
	req, err := http.NewRequest("POST", "", strings.NewReader(invalidJSON))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	dest := &TestStructNonPointer{}
	err = Bind(req, dest)

	require.Error(t, err)
}

// TestBindNumericAndBool verifies that numeric and boolean form fields are correctly bound.
func TestBindNumericAndBool(t *testing.T) {
	t.Parallel()

	type NumericAndBool struct {
		UintVal  uint    `form:"uint_val"`
		FloatVal float64 `form:"float_val"`
		BoolVal  bool    `form:"bool_val"`
	}

	form := url.Values{
		"uint_val":  {"42"},
		"float_val": {"3.14159"},
		"bool_val":  {"true"},
	}
	req, err := http.NewRequest("POST", "", strings.NewReader(form.Encode()))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	dest := &NumericAndBool{}

	require.NoError(t, Bind(req, dest))

	assert.Equal(t, uint(42), dest.UintVal)
	assert.Equal(t, 3.14159, dest.FloatVal)
	assert.Equal(t, true, dest.BoolVal)
}

// TestBindNumericErrors verifies that binding errors occur when numeric conversion fails.
func TestBindNumericErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		formValue map[string]string
		fieldName string
	}{
		{"invalid int", map[string]string{"age": "invalid"}, "age"},
		{"invalid uint", map[string]string{"uint_val": "-1"}, "uint_val"},
		{"invalid float", map[string]string{"float_val": "not-a-number"}, "float_val"},
		{"invalid bool", map[string]string{"bool_val": "not-a-bool"}, "bool_val"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			form := url.Values{}
			for k, v := range tc.formValue {
				form.Set(k, v)
			}

			req, err := http.NewRequest("POST", "", strings.NewReader(form.Encode()))

			require.NoError(t, err)

			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			switch tc.fieldName {
			case "age":
				dest := &TestStructNonPointer{}
				err := Bind(req, dest)
				require.Error(t, err)
			case "uint_val", "float_val", "bool_val":
				dest := &struct {
					UintVal  uint    `form:"uint_val"`
					FloatVal float64 `form:"float_val"`
					BoolVal  bool    `form:"bool_val"`
				}{}
				err := Bind(req, dest)

				require.Error(t, err)
			}
		})
	}
}

// TestBindEmptyFormValues verifies that binding empty form values does not overwrite existing struct values.
func TestBindEmptyFormValues(t *testing.T) {
	t.Parallel()

	dest := &TestStructNonPointer{
		Name:  "Initial Name",
		Age:   20,
		Email: "initial@example.com",
	}

	form := url.Values{
		"name":  {""},
		"age":   {""},
		"email": {""},
	}
	req, err := http.NewRequest("POST", "", strings.NewReader(form.Encode()))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	require.NoError(t, Bind(req, dest))

	assert.Equal(t, "Initial Name", dest.Name)
	assert.Equal(t, 20, dest.Age)
	assert.Equal(t, "initial@example.com", dest.Email)
}

// TestBinderError verifies that a binder returning an error is correctly propagated.
// We use a failing binder in a struct field to trigger the error.
func TestBinderError(t *testing.T) {
	t.Parallel()

	type BinderStruct struct {
		Value *failingBinder `form:"value"`
	}
	form := url.Values{
		"value": {"test"},
	}
	req, err := http.NewRequest("POST", "", strings.NewReader(form.Encode()))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	dest := &BinderStruct{}
	err = Bind(req, dest)

	require.Error(t, err)

	assert.Contains(t, err.Error(), "binding error")
}

// TestDefaultFormFieldNames verifies that struct fields without form tags use default field name mapping.
func TestDefaultFormFieldNames(t *testing.T) {
	t.Parallel()

	type NoFormTags struct {
		FirstName string
		LastName  string
		Age       int
	}

	form := url.Values{
		"firstname": {"John"},
		"lastname":  {"Doe"},
		"age":       {"42"},
	}
	req, err := http.NewRequest("POST", "", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	dest := &NoFormTags{}
	require.NoError(t, Bind(req, dest))

	assert.Equal(t, "John", dest.FirstName)
	assert.Equal(t, "Doe", dest.LastName)
	assert.Equal(t, 42, dest.Age)
}

// TestInvalidDestinationType verifies that Bind returns an error for unsupported destination types.
func TestInvalidDestinationType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		dest interface{}
	}{
		{"non-pointer", TestStructNonPointer{}},
		{"pointer to non-struct", new(string)},
		{"nil", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			form := url.Values{}
			req, err := http.NewRequest("POST", "", strings.NewReader(form.Encode()))

			require.NoError(t, err)

			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			err = Bind(req, tc.dest)

			require.Error(t, err)
			assert.ErrorIs(t, err, errInvalidType)
		})
	}
}

// TestParseFormError verifies that Bind returns an error when form parsing fails,
// iotest.ErrReader is used to simulate a read error.
func TestParseFormError(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest("POST", "", iotest.ErrReader(errors.New("forced read error")))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	dest := &TestStructNonPointer{}
	err = Bind(req, dest)

	require.Error(t, err)
}

// TestUnsupportedFieldTypes verifies that Bind returns an error for unsupported field types.
func TestUnsupportedFieldTypes(t *testing.T) {
	t.Parallel()

	type UnsupportedType struct {
		Unsupported map[string]string `form:"unsupported"`
	}

	form := url.Values{
		"unsupported": {"some-value"},
	}
	req, err := http.NewRequest("POST", "", strings.NewReader(form.Encode()))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	dest := &UnsupportedType{}
	err = Bind(req, dest)

	require.Error(t, err)

	assert.ErrorIs(t, err, errInvalidType)
}

// failingBinder is a minimal type that always returns an error during binding.
type failingBinder struct{}

func (f *failingBinder) Bind(string) error {
	return errors.New("binding error")
}
