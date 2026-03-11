package codec

import (
	"encoding/json"
	"io"

	apiv1 "github.com/attestantio/go-builder-client/api/v1"
	"github.com/pkg/errors"
)

// UnmarshalValidatorRegistrations unmarshals validator registrations from JSON or SSZ.
func UnmarshalValidatorRegistrations(contentType string, body io.Reader) ([]*apiv1.SignedValidatorRegistration, error) {
	switch contentType {
	case MediaTypeJSON:
		var regs []*apiv1.SignedValidatorRegistration
		if err := json.NewDecoder(body).Decode(&regs); err != nil {
			return nil, errors.Wrap(err, "invalid JSON")
		}
		for _, r := range regs {
			if r == nil {
				return nil, errors.New("nil registration")
			}
			if r.Message == nil {
				return nil, errors.New("nil registration message")
			}
		}
		return regs, nil

	case MediaTypeSSZ:
		raw, err := io.ReadAll(body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read body")
		}
		var regs apiv1.SignedValidatorRegistrations
		if err := regs.UnmarshalSSZ(raw); err != nil {
			return nil, errors.Wrap(err, "invalid SSZ")
		}
		for _, r := range regs.Registrations {
			if r == nil {
				return nil, errors.New("nil registration")
			}
			if r.Message == nil {
				return nil, errors.New("nil registration message")
			}
		}
		return regs.Registrations, nil

	default:
		return nil, UnsupportedContentTypeError{ContentType: contentType}
	}
}
