package storage

import (
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"
	"testing"
)

func TestExporterStorage_SaveAndGetSyncOffset(t *testing.T) {
	s, done := newStorageForTest()
	require.NotNil(t, s)
	defer done()

	offset := new(big.Int)
	offset.SetString("49e08f", 16)
	err := s.SaveSyncOffset(offset)
	require.NoError(t, err)

	o, err := s.GetSyncOffset()
	require.NoError(t, err)
	require.Zero(t, offset.Cmp(o))
}

func newStorageForTest() (Storage, func()) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := NewExporterStorage(db, logger)
	return s, func() {
		db.Close()
	}
}

//
//func TestToValidatorInformation(t *testing.T) {
//	bls.Init(bls.BLS12_381)
//
//	pk := bls.PublicKey{}
//	//pk.Deserialize()
//	dec, err := hex.DecodeString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACZExTMHRMUzFDUlVkSlRpQlNVMEVnVUZWQ1RFbERJRXRGV1MwdExTMHRDazFKU1VKSmFrRk9RbWRyY1docmFVYzVkekJDUVZGRlJrRkJUME5CVVRoQlRVbEpRa05uUzBOQlVVVkJOMnBYY0V4cmVtZDJUWGR2UnpoTmRFVnlVaklLUkdoVU1rMTFkRWxtWVVkMFZteE1lRFZXSzJnNGFtd3Jkbmx4VDFZdmNteEtSRVZsUXk5SE16VnBWME0wV0VVM1JuRktVVmMxUW1wdlFXWjFUWGhRZWdwUlF6WjZNRUUxYjFJM2VuUnVXSFUyYzBWM1RraEpTRmgzUkVGSVRIbFRkVmRRTTNCR1lsbzBRbmM1YjFGWlRVSm1iVk5zTDNoWFIwc3lWbk4zYVZoa0NrTkZjVVpLUm1kTlVGazNObEpRWTBvMlIyZGtUV2NyV1ZSUldWVkZhbWxSVGpGcGRtSktaalJXYVVwQ1JUY3JiVk50ZUZaTk5UQXpWbWx5UVdabmRrSUtlbkJuZFROemRIWklkSHBSVjFaMmVISjBOVFIwUm05RE1IUm1XRTFSUlhOU1UwVnRUVlJvVmtob2NWb3JaVEpDT0M5a1RXUTJSMUZvZG5FNVpYUjFSUXBoUWt4b1NscEZVWGxwTWtscFVVMDJVbGcyYTAxdlpHZEdVbWN2ZW10dFRGWlhRMFZJVHpFemFGVjVSa294YW5nMUwwTTViRUl5VTJWRU5XOWpkMWg0Q21KUlNVUkJVVUZDQ2kwdExTMHRSVTVFSUZKVFFTQlFWVUpNU1VNZ1MwVlpMUzB0TFMwSwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
//	require.NoError(t, err)
//	fmt.Printf("%x", dec)
//}
