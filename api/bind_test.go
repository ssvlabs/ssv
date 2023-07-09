package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type CSV []string

func (c *CSV) Bind(value string) error {
	*c = CSV(strings.Split(value, ","))
	return nil
}

type TestStructNonPointer struct {
	Name  string `form:"name"`
	Age   int    `form:"age"`
	Email string `form:"email"`
	Tags  CSV    `form:"tags"`
}

type TestStructPointer struct {
	Name  string `form:"name"`
	Age   int    `form:"age"`
	Email string `form:"email"`
	Tags  *CSV   `form:"tags"`
}

func runTestBindForm(t *testing.T, dest interface{}, validate func(*testing.T, interface{})) {
	form := url.Values{
		"name":  []string{"John Doe"},
		"age":   []string{"30"},
		"email": []string{"john.doe@example.com"},
		"tags":  []string{"tag1,tag2,tag3"},
	}
	req, _ := http.NewRequest("POST", "", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if err := Bind(req, dest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	validate(t, dest)
}

func TestBindFormNonPointer(t *testing.T) {
	dest := &TestStructNonPointer{}
	validate := func(t *testing.T, dest interface{}) {
		s, ok := dest.(*TestStructNonPointer)
		assert.True(t, ok)
		assert.Equal(t, "John Doe", s.Name)
		assert.Equal(t, 30, s.Age)
		assert.Equal(t, "john.doe@example.com", s.Email)
		assert.Equal(t, CSV{"tag1", "tag2", "tag3"}, s.Tags)
	}
	runTestBindForm(t, dest, validate)
}

func TestBindFormPointer(t *testing.T) {
	dest := &TestStructPointer{}
	validate := func(t *testing.T, dest interface{}) {
		s, ok := dest.(*TestStructPointer)
		assert.True(t, ok)
		assert.Equal(t, "John Doe", s.Name)
		assert.Equal(t, 30, s.Age)
		assert.Equal(t, "john.doe@example.com", s.Email)
		assert.Equal(t, &CSV{"tag1", "tag2", "tag3"}, s.Tags)
	}
	runTestBindForm(t, dest, validate)
}
