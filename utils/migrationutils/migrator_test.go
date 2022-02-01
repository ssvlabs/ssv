package migrationutils

import (
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func TestMigrate(t *testing.T) {
	tmpPath, err := ioutil.TempDir("", "xxx")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(tmpPath, 0700))
	clean, err := Migrate(tmpPath)
	require.NoError(t, err)
	require.True(t, clean)
	// second time should be false
	clean, err = Migrate(tmpPath)
	require.NoError(t, err)
	require.False(t, clean)
	// deleting second file
	require.NoError(t, os.RemoveAll(path.Join(tmpPath, "oa_pks")))
	clean, err = Migrate(tmpPath)
	require.NoError(t, err)
	require.True(t, clean)
}
