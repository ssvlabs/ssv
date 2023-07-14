package sharemap

import (
	"bytes"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	"github.com/stretchr/testify/require"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func TestNew(t *testing.T) {
	s := New()
	require.NotNil(t, s)
	require.IsType(t, &hashmap.Map[string, *ssvtypes.SSVShare]{}, s.shares)
}

func TestGet(t *testing.T) {
	s := New()

	pk := []byte("test")
	share := &ssvtypes.SSVShare{Share: spectypes.Share{ValidatorPubKey: pk}}
	s.Save(share)

	require.Equal(t, share, s.Get(pk))

	nonexistentPK := []byte("nonexistent")
	require.Nil(t, s.Get(nonexistentPK))
}

func TestList(t *testing.T) {
	s := New()

	pk1 := []byte("test1")
	pk2 := []byte("test2")
	share1 := &ssvtypes.SSVShare{Share: spectypes.Share{ValidatorPubKey: pk1}}
	share2 := &ssvtypes.SSVShare{Share: spectypes.Share{ValidatorPubKey: pk2}}
	s.Save(share1)
	s.Save(share2)

	result := s.List()
	require.Contains(t, result, share1)
	require.Contains(t, result, share2)

	result = s.List(func(share *ssvtypes.SSVShare) bool { return bytes.Equal(share.ValidatorPubKey, pk1) })
	require.Contains(t, result, share1)
	require.NotContains(t, result, share2)
}

func TestSave(t *testing.T) {
	s := New()

	pk := []byte("test")
	share := &ssvtypes.SSVShare{Share: spectypes.Share{ValidatorPubKey: pk}}
	s.Save(share)

	result := s.Get(pk)
	require.NotNil(t, result)
	require.Equal(t, share, result)
}

func TestDelete(t *testing.T) {
	s := New()

	pk := []byte("test")
	share := &ssvtypes.SSVShare{Share: spectypes.Share{ValidatorPubKey: pk}}
	s.Save(share)

	s.Delete(pk)

	result := s.Get(pk)
	require.Nil(t, result)
}
