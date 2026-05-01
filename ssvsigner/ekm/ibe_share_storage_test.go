package ekm

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

func sampleClusterID(t *testing.T, seed byte) [32]byte {
	t.Helper()
	var out [32]byte
	for i := range out {
		out[i] = seed
	}
	return out
}

func sampleRecord() *IBEShareRecord {
	return &IBEShareRecord{
		Generation:       3,
		ShareBytes:       []byte{0xa1, 0xa2, 0xa3, 0xa4},
		ClusterIBEPubKey: []byte{0xb1, 0xb2, 0xb3, 0xb4, 0xb5},
		PolyCommits: [][]byte{
			{0xc0},
			{0xc1, 0xc2},
			{0xc3, 0xc4, 0xc5},
		},
	}
}

func TestIBEShare_RoundTrip(t *testing.T) {
	store, cleanup := newStorageForTest(t)
	defer cleanup()

	cid := sampleClusterID(t, 0x01)
	in := sampleRecord()
	require.NoError(t, store.SaveIBEShare(cid, in))

	out, err := store.GetIBEShare(cid)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, in.Generation, out.Generation)
	require.Equal(t, in.ShareBytes, out.ShareBytes)
	require.Equal(t, in.ClusterIBEPubKey, out.ClusterIBEPubKey)
	require.Equal(t, in.PolyCommits, out.PolyCommits)
}

func TestIBEShare_RoundTripEncrypted(t *testing.T) {
	store, cleanup := newStorageForTest(t)
	defer cleanup()
	store.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef"))

	cid := sampleClusterID(t, 0x02)
	in := sampleRecord()
	require.NoError(t, store.SaveIBEShare(cid, in))

	out, err := store.GetIBEShare(cid)
	require.NoError(t, err)
	require.Equal(t, in.Generation, out.Generation)
	require.Equal(t, in.ShareBytes, out.ShareBytes)
	require.Equal(t, in.ClusterIBEPubKey, out.ClusterIBEPubKey)
	require.Equal(t, in.PolyCommits, out.PolyCommits)
}

func TestIBEShare_GetMissingReturnsNotFound(t *testing.T) {
	store, cleanup := newStorageForTest(t)
	defer cleanup()

	out, err := store.GetIBEShare(sampleClusterID(t, 0xff))
	require.Nil(t, out)
	require.True(t, errors.Is(err, ErrIBEShareNotFound))
}

func TestIBEShare_Overwrite(t *testing.T) {
	store, cleanup := newStorageForTest(t)
	defer cleanup()

	cid := sampleClusterID(t, 0x03)
	first := sampleRecord()
	require.NoError(t, store.SaveIBEShare(cid, first))

	second := sampleRecord()
	second.Generation = first.Generation + 1
	second.ShareBytes = []byte{0xde, 0xad, 0xbe, 0xef}
	require.NoError(t, store.SaveIBEShare(cid, second))

	out, err := store.GetIBEShare(cid)
	require.NoError(t, err)
	require.Equal(t, second.Generation, out.Generation)
	require.Equal(t, second.ShareBytes, out.ShareBytes)
}

func TestIBEShare_RemoveIdempotent(t *testing.T) {
	store, cleanup := newStorageForTest(t)
	defer cleanup()

	cid := sampleClusterID(t, 0x04)
	require.NoError(t, store.SaveIBEShare(cid, sampleRecord()))

	require.NoError(t, store.RemoveIBEShare(cid))
	_, err := store.GetIBEShare(cid)
	require.True(t, errors.Is(err, ErrIBEShareNotFound))

	// Removing again is fine.
	require.NoError(t, store.RemoveIBEShare(cid))
}

func TestIBEShare_SaveNilRecord(t *testing.T) {
	store, cleanup := newStorageForTest(t)
	defer cleanup()

	require.Error(t, store.SaveIBEShare(sampleClusterID(t, 0x05), nil))
}

func TestIBEShare_LocalKeyManager_RoundTrip(t *testing.T) {
	initBLSTest()

	logger := testLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	defer db.Close()

	pk, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, err := NewLocalKeyManager(logger, db, testBeaconConfig(), pk)
	require.NoError(t, err)

	cid := sampleClusterID(t, 0x42)
	share := []byte("share-bytes")
	pubkey := []byte("ibe-pubkey")
	commits := [][]byte{pubkey, []byte("c1"), []byte("c2")}

	require.NoError(t, km.AddIBEShare(cid, 7, share, pubkey, commits))

	gotShare, err := km.GetIBEShareBytes(cid)
	require.NoError(t, err)
	require.Equal(t, share, gotShare)

	gotPub, err := km.GetClusterIBEPubKey(cid)
	require.NoError(t, err)
	require.Equal(t, pubkey, gotPub)

	gotCommits, err := km.GetClusterIBEPolyCommits(cid)
	require.NoError(t, err)
	require.Equal(t, commits, gotCommits)

	// Mutating the returned slice doesn't affect the stored record
	// (defensive copy).
	gotShare[0] = 0xff
	again, err := km.GetIBEShareBytes(cid)
	require.NoError(t, err)
	require.Equal(t, share, again)

	require.NoError(t, km.RemoveIBEShare(cid))
	_, err = km.GetIBEShareBytes(cid)
	require.True(t, errors.Is(err, ErrIBEShareNotFound))
}

func TestIBEShare_LocalKeyManager_RejectsEmpty(t *testing.T) {
	initBLSTest()

	logger := testLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	defer db.Close()

	pk, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, err := NewLocalKeyManager(logger, db, testBeaconConfig(), pk)
	require.NoError(t, err)

	cid := sampleClusterID(t, 0x55)
	commits := [][]byte{{0xaa}}
	require.Error(t, km.AddIBEShare(cid, 0, nil, []byte("p"), commits))
	require.Error(t, km.AddIBEShare(cid, 0, []byte("s"), nil, commits))
	require.Error(t, km.AddIBEShare(cid, 0, []byte("s"), []byte("p"), nil))
}

func TestIBEShare_DistinctClusters(t *testing.T) {
	store, cleanup := newStorageForTest(t)
	defer cleanup()

	a := sampleClusterID(t, 0x10)
	b := sampleClusterID(t, 0x20)

	recA := sampleRecord()
	recA.ShareBytes = []byte{0x0a}
	recB := sampleRecord()
	recB.ShareBytes = []byte{0x0b}

	require.NoError(t, store.SaveIBEShare(a, recA))
	require.NoError(t, store.SaveIBEShare(b, recB))

	gotA, err := store.GetIBEShare(a)
	require.NoError(t, err)
	require.Equal(t, []byte{0x0a}, gotA.ShareBytes)

	gotB, err := store.GetIBEShare(b)
	require.NoError(t, err)
	require.Equal(t, []byte{0x0b}, gotB.ShareBytes)
}
