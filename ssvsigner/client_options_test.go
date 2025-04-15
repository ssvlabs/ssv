package ssvsigner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestClientOption_WithLogger(t *testing.T) {
	t.Parallel()

	logger := zap.NewExample()
	client := &Client{}

	opt := WithLogger(logger)
	opt(client)

	assert.Equal(t, logger, client.logger)
}

func TestClientOption_WithClientCert(t *testing.T) {
	t.Parallel()

	certData := []byte("test-certificate")
	client := &Client{}

	opt := WithClientCert(certData)
	opt(client)

	assert.Equal(t, certData, client.clientCert)
}

func TestClientOption_WithClientKey(t *testing.T) {
	t.Parallel()

	keyData := []byte("test-key")
	client := &Client{}

	opt := WithClientKey(keyData)
	opt(client)

	assert.Equal(t, keyData, client.clientKey)
}

func TestClientOption_WithCACert(t *testing.T) {
	t.Parallel()

	caCertData := []byte("test-ca-certificate")
	client := &Client{}

	opt := WithCACert(caCertData)
	opt(client)

	assert.Equal(t, caCertData, client.caCert)
}

func TestNewClient_WithAllOptions(t *testing.T) {
	t.Parallel()

	const baseURL = "https://test.example.com"
	logger := zap.NewExample()
	certData := []byte("")
	keyData := []byte("")
	caCertData := []byte("")

	client, err := NewClient(
		baseURL,
		WithLogger(logger),
		WithClientCert(certData),
		WithClientKey(keyData),
		WithCACert(caCertData),
	)
	require.NoError(t, err)
	require.NotNil(t, client)

	assert.Equal(t, logger, client.logger)
	assert.Equal(t, certData, client.clientCert)
	assert.Equal(t, keyData, client.clientKey)
	assert.Equal(t, caCertData, client.caCert)
	assert.Equal(t, baseURL, client.baseURL)
}

func TestNewClient_TrimsTrailingSlashFromURL(t *testing.T) {
	t.Parallel()

	const inputURL = "https://test.example.com/"
	const expectedURL = "https://test.example.com"

	client, err := NewClient(inputURL)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, expectedURL, client.baseURL)
}
