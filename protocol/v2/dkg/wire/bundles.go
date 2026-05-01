package wire

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/drand/kyber"
	"github.com/drand/kyber/share/dkg"
)

// DealEnvelope wraps a kyber DKG DealBundle with SSV-side routing fields.
// The DealBundle itself is what kyber's protocol produces in the deal
// phase; ClusterID + Generation route it to the right ceremony state.
type DealEnvelope struct {
	ClusterID  [32]byte
	Generation uint64
	Bundle     *dkg.DealBundle
}

// ResponseEnvelope wraps a kyber DKG ResponseBundle with SSV-side routing
// fields. ResponseBundle has no kyber.Point / kyber.Scalar fields, so its
// JSON encoding is direct (mirroring ssvlabs/ssv-dkg's pkgs/wire/response.go).
type ResponseEnvelope struct {
	ClusterID  [32]byte
	Generation uint64
	Bundle     *dkg.ResponseBundle
}

// JustificationEnvelope wraps a kyber DKG JustificationBundle with SSV-side
// routing fields.
type JustificationEnvelope struct {
	ClusterID  [32]byte
	Generation uint64
	Bundle     *dkg.JustificationBundle
}

// ---- Deal bundle JSON encoding ----------------------------------------
//
// kyber.Point fields (DealBundle.Public) are not JSON-marshalable directly;
// they're hex-encoded as their MarshalBinary output. The rest of the
// DealBundle fields are JSON-friendly.

type dealEnvelopeJSON struct {
	ClusterID  string         `json:"cluster_id"`
	Generation uint64         `json:"generation"`
	Bundle     dealBundleJSON `json:"bundle"`
}

type dealBundleJSON struct {
	DealerIndex uint32     `json:"dealer_index"`
	Deals       []dkg.Deal `json:"deals"`
	Public      []string   `json:"public"`
	SessionID   []byte     `json:"session_id"`
	Signature   []byte     `json:"signature"`
}

// EncodeDealEnvelope serializes a DealEnvelope to JSON bytes. Public
// kyber.Point values inside the DealBundle are hex-encoded.
func EncodeDealEnvelope(d *DealEnvelope) ([]byte, error) {
	if d == nil || d.Bundle == nil {
		return nil, errors.New("wire: nil deal envelope or bundle")
	}
	publics := make([]string, 0, len(d.Bundle.Public))
	for _, p := range d.Bundle.Public {
		b, err := p.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("wire: marshal deal-bundle public point: %w", err)
		}
		publics = append(publics, hex.EncodeToString(b))
	}
	out := dealEnvelopeJSON{
		ClusterID:  hex.EncodeToString(d.ClusterID[:]),
		Generation: d.Generation,
		Bundle: dealBundleJSON{
			DealerIndex: d.Bundle.DealerIndex,
			Deals:       d.Bundle.Deals,
			Public:      publics,
			SessionID:   d.Bundle.SessionID,
			Signature:   d.Bundle.Signature,
		},
	}
	return json.Marshal(out)
}

// DecodeDealEnvelope parses DealEnvelope JSON bytes back into a
// *DealEnvelope. The kyber suite is required to deserialize the public
// polynomial points back into kyber.Point.
func DecodeDealEnvelope(data []byte, suite KyberSuite) (*DealEnvelope, error) {
	var in dealEnvelopeJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("wire: unmarshal deal envelope json: %w", err)
	}
	cid, err := decodeClusterIDHex(in.ClusterID)
	if err != nil {
		return nil, err
	}
	publics := make([]kyber.Point, 0, len(in.Bundle.Public))
	for i, hexStr := range in.Bundle.Public {
		b, err := hex.DecodeString(hexStr)
		if err != nil {
			return nil, fmt.Errorf("wire: decode deal-bundle public[%d] hex: %w", i, err)
		}
		pt := suite.Point()
		if err := pt.UnmarshalBinary(b); err != nil {
			return nil, fmt.Errorf("wire: unmarshal deal-bundle public[%d]: %w", i, err)
		}
		publics = append(publics, pt)
	}
	out := &DealEnvelope{
		ClusterID:  cid,
		Generation: in.Generation,
		Bundle: &dkg.DealBundle{
			DealerIndex: in.Bundle.DealerIndex,
			Deals:       in.Bundle.Deals,
			Public:      publics,
			SessionID:   in.Bundle.SessionID,
			Signature:   in.Bundle.Signature,
		},
	}
	return out, nil
}

// ---- Response bundle JSON encoding ------------------------------------

type responseEnvelopeJSON struct {
	ClusterID  string              `json:"cluster_id"`
	Generation uint64              `json:"generation"`
	Bundle     *dkg.ResponseBundle `json:"bundle"`
}

// EncodeResponseEnvelope serializes a ResponseEnvelope to JSON bytes.
// ResponseBundle has no kyber-typed fields, so it round-trips through
// stdlib JSON directly.
func EncodeResponseEnvelope(r *ResponseEnvelope) ([]byte, error) {
	if r == nil || r.Bundle == nil {
		return nil, errors.New("wire: nil response envelope or bundle")
	}
	out := responseEnvelopeJSON{
		ClusterID:  hex.EncodeToString(r.ClusterID[:]),
		Generation: r.Generation,
		Bundle:     r.Bundle,
	}
	return json.Marshal(out)
}

// DecodeResponseEnvelope parses ResponseEnvelope JSON bytes back into a
// *ResponseEnvelope.
func DecodeResponseEnvelope(data []byte) (*ResponseEnvelope, error) {
	var in responseEnvelopeJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("wire: unmarshal response envelope json: %w", err)
	}
	if in.Bundle == nil {
		return nil, errors.New("wire: response envelope missing bundle")
	}
	cid, err := decodeClusterIDHex(in.ClusterID)
	if err != nil {
		return nil, err
	}
	return &ResponseEnvelope{
		ClusterID:  cid,
		Generation: in.Generation,
		Bundle:     in.Bundle,
	}, nil
}

// ---- Justification bundle JSON encoding -------------------------------
//
// JustificationBundle.Justifications has kyber.Scalar fields; they're hex-
// encoded as their MarshalBinary output, mirroring the DealBundle pattern.

type justificationEnvelopeJSON struct {
	ClusterID  string                  `json:"cluster_id"`
	Generation uint64                  `json:"generation"`
	Bundle     justificationBundleJSON `json:"bundle"`
}

type justificationBundleJSON struct {
	DealerIndex    uint32              `json:"dealer_index"`
	Justifications []justificationJSON `json:"justifications"`
	SessionID      []byte              `json:"session_id"`
	Signature      []byte              `json:"signature"`
}

type justificationJSON struct {
	ShareIndex uint32 `json:"share_index"`
	Share      string `json:"share"`
}

// EncodeJustificationEnvelope serializes a JustificationEnvelope to JSON
// bytes. Each Justification.Share kyber.Scalar is hex-encoded.
func EncodeJustificationEnvelope(j *JustificationEnvelope) ([]byte, error) {
	if j == nil || j.Bundle == nil {
		return nil, errors.New("wire: nil justification envelope or bundle")
	}
	js := make([]justificationJSON, 0, len(j.Bundle.Justifications))
	for _, jj := range j.Bundle.Justifications {
		if jj.Share == nil {
			return nil, errors.New("wire: justification has nil share scalar")
		}
		b, err := jj.Share.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("wire: marshal justification share scalar: %w", err)
		}
		js = append(js, justificationJSON{
			ShareIndex: jj.ShareIndex,
			Share:      hex.EncodeToString(b),
		})
	}
	out := justificationEnvelopeJSON{
		ClusterID:  hex.EncodeToString(j.ClusterID[:]),
		Generation: j.Generation,
		Bundle: justificationBundleJSON{
			DealerIndex:    j.Bundle.DealerIndex,
			Justifications: js,
			SessionID:      j.Bundle.SessionID,
			Signature:      j.Bundle.Signature,
		},
	}
	return json.Marshal(out)
}

// DecodeJustificationEnvelope parses JustificationEnvelope JSON bytes back
// into a *JustificationEnvelope. The kyber suite is required to
// deserialize Share scalars back into kyber.Scalar.
func DecodeJustificationEnvelope(data []byte, suite KyberSuite) (*JustificationEnvelope, error) {
	var in justificationEnvelopeJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("wire: unmarshal justification envelope json: %w", err)
	}
	cid, err := decodeClusterIDHex(in.ClusterID)
	if err != nil {
		return nil, err
	}
	js := make([]dkg.Justification, 0, len(in.Bundle.Justifications))
	for i, jj := range in.Bundle.Justifications {
		b, err := hex.DecodeString(jj.Share)
		if err != nil {
			return nil, fmt.Errorf("wire: decode justification[%d] share hex: %w", i, err)
		}
		s := suite.Scalar()
		if err := s.UnmarshalBinary(b); err != nil {
			return nil, fmt.Errorf("wire: unmarshal justification[%d] share scalar: %w", i, err)
		}
		js = append(js, dkg.Justification{
			ShareIndex: jj.ShareIndex,
			Share:      s,
		})
	}
	return &JustificationEnvelope{
		ClusterID:  cid,
		Generation: in.Generation,
		Bundle: &dkg.JustificationBundle{
			DealerIndex:    in.Bundle.DealerIndex,
			Justifications: js,
			SessionID:      in.Bundle.SessionID,
			Signature:      in.Bundle.Signature,
		},
	}, nil
}

// ---- helpers ----------------------------------------------------------

func decodeClusterIDHex(s string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, fmt.Errorf("wire: decode cluster_id hex: %w", err)
	}
	if len(b) != 32 {
		return out, fmt.Errorf("wire: cluster_id must be 32 bytes, got %d", len(b))
	}
	copy(out[:], b)
	return out, nil
}
