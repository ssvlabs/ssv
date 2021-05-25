package collections

import (
	"encoding/base64"
	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

var (
	pkPem  = "LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBb3dFN09FYnd5TGt2clowVFU0amoKb295SUZ4TnZnclk4RmorV3NseVpUbHlqOFVEZkZyWWg1VW4ydTRZTWRBZStjUGYxWEsrQS9QOVhYN09CNG5mMQpPb0dWQjZ3ckMvamhMYnZPSDY1MHJ5VVlvcGVZaGxTWHhHbkQ0dmN2VHZjcUxMQit1ZTIvaXlTeFFMcFpSLzZWCnNUM2ZGckVvbnpGVHFuRkN3Q0YyOGlQbkpWQmpYNlQvSGNUSjU1SURrYnRvdGFyVTZjd3dOT0huSGt6V3J2N2kKdHlQa1I0R2UxMWhtVkc5UWpST3Q1NmVoWGZGc0ZvNU1xU3ZxcFlwbFhrSS96VU5tOGovbHFFZFUwUlhVcjQxTAoyaHlLWS9wVmpzZ21lVHNONy9acUFDa0h5ZTlGYmtWOVYvVmJUaDdoV1ZMVHFHU2g3QlkvRDdnd093ZnVLaXEyClR3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K"
	skPem  = "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFcFFJQkFBS0NBUUVBb3dFN09FYnd5TGt2clowVFU0ampvb3lJRnhOdmdyWThGaitXc2x5WlRseWo4VURmCkZyWWg1VW4ydTRZTWRBZStjUGYxWEsrQS9QOVhYN09CNG5mMU9vR1ZCNndyQy9qaExidk9INjUwcnlVWW9wZVkKaGxTWHhHbkQ0dmN2VHZjcUxMQit1ZTIvaXlTeFFMcFpSLzZWc1QzZkZyRW9uekZUcW5GQ3dDRjI4aVBuSlZCagpYNlQvSGNUSjU1SURrYnRvdGFyVTZjd3dOT0huSGt6V3J2N2l0eVBrUjRHZTExaG1WRzlRalJPdDU2ZWhYZkZzCkZvNU1xU3ZxcFlwbFhrSS96VU5tOGovbHFFZFUwUlhVcjQxTDJoeUtZL3BWanNnbWVUc043L1pxQUNrSHllOUYKYmtWOVYvVmJUaDdoV1ZMVHFHU2g3QlkvRDdnd093ZnVLaXEyVHdJREFRQUJBb0lCQURqTzNReW43SktIdDQ0UwpDQUk4MnRoemtabzVNOHVpSng2NTJwTWVvbThrNmgzU05lMThYQ1BFdXpCdmJ6ZWcyMFlUcEhkQTB2dFpJZUpBCmRTdXdFczdwQ2o4NlNXWkt2bTlwM0ZRK1FId3B1WVF3d1A5UHkvU3Z4NHo2Q0lyRXFQWWFMSkF2dzJtQ3lDTisKems3QTh2cHFUYTFpNEgxYWU0WVRJdWhDd1dseGUxdHRENnJWVVlmQzJyVmFGSitiOEpsekZScTRibkFSOHltZQpyRTRpQWxmZ1RPajl6TDgxNHFSbFlRZWVaaE12QThUMHFXVW9oYnIxaW1vNVh6SUpaYXlMb2N2cWhaRWJrMGRqCnE5cUtXZElwQUFUUmpXdmIrN1Bram1sd05qTE9oSjFwaHRDa2MvUzRqMmN2bzlnY1M3V2FmeGFxQ2wvaXg0WXQKNUt2UEo4RUNnWUVBMEVtNG5NTUVGWGJ1U00vbDVVQ3p2M2tUNkgvVFlPN0ZWaDA3MUc3UUFGb2xveEpCWkRGVgo3ZkhzYyt1Q2ltbEcyWHQzQ3JHbzl0c09uRi9aZ0RLTm10RHZ2anhtbFBuQWI1ZzR1aFhnWU5Nc0tRU2hwZVJXCi9heThDbVdic1JxWFphTG9JNWJyMmtDVEx3c1Z6MmhwYWJBekJPcjJZVjN2TVJCNWk3Q09ZU01DZ1lFQXlGZ0wKM0RrS3dzVFR5VnlwbGVub0FaYVMvbzBtS3habmZmUm5ITlA1UWdSZlQ0cFFrdW9naytNWUFlQnVHc2M0Y1RpNwpyVHR5dFVNQkFCWEVLR0lKa0FiTm9BU0hRTVVjTzF2dmN3aEJXN0F5K294dWMwSlNsbmFYam93UzBDMG8vNHFyClEvcnBVbmVpcitWdS9OOCs2ZWRFVFJrTmorNXVubWVQRWU5TkJ1VUNnWUVBZ3RVcjMxd29Ib3Q4RmNSeE5kVzAKa3BzdFJDZTIwUFpxZ2pNT3Q5dDdVQjFQOHVTdXFvN0syUkhUWXVVV05IYjRoL2VqeU5YYnVtUFRBNnE1Wm10YQp3MXBtbldvM1RYQ3J6ZTBpQk5GbEJhemYya3dNZGJXK1pzMnZ1Q0FtOGRJd015bG5BNlB6Tmo3RnRSRVRmQnFyCnpEVmZkc0ZZVGNUQlVHSjIxcVhxYVYwQ2dZRUFtdU1QRUV2OVdNVG80M1ZER3NhQ2VxL1pwdmlpK0k3U3Boc00KbU1uOG02QmJ1MWU0b1V4bXNVN1JvYW5NRmVITmJpTXBYVzFuYW1HSjVYSHVmRFlISkpWTjVaZDZwWVYrSlJvWApqanhrb3lrZTBIcy9iTlpxbVM3SVR3bFdCaUhUMzNScW9oemF3OG9BT2JMTVVxMlpxeVlEdFFOWWE5MHZJa0gzCjV5cTF4MDBDZ1lFQXM0enRRaEdSYmVVbHFuVzZaNnlmUko2WFhZcWRNUGh4dUJ4dk5uL2R4SjEwVDRXMkRVdUMKalNkcEdYclkrRUNZeVhVd2xYQnFiYUt4MUs1QVFEN25tdTlKM2wwb01rWDZ0U0JqMU9FNU1hYkFUcnNXNnd2VApoa1RQSlpNeVBVWWhvQmtpdlBVS3lRWHN3clFWL25VUUFzQWNMZUpTaFRXNGdTczBNNndlUUFjPQotLS0tLUVORCBSU0EgUFJJVkFURSBLRVktLS0tLQo="
	skPem2 = "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFb3dJQkFBS0NBUUVBejJQUkdwNmhUbm5ESXRLTnhMQUNVMkpEOHhLcHpXdUtFK01BcUMwTU9DK3IwdmRYCkdhMkdTcGJXbXhMc1EvcTRWajRFcmpaWFliSGRKZWNkTVJJM1kzVjl1bU9sRGJBZjNxMWs3M2FEbE04U2tBbjMKektBOWtzQkpVejczNkw5UVgxU3FpWUN5S1o5V2V0d1NOclN0blczVUE3WHd1T2VmeWVFUkg0aERYclFIeVZuQgpReDFxdlZibkl6SE15Y09oaWsxbzVocUo0QzJEZjhHWUV5Nlk0czNhTnBkVHgwWFNWS2hWb3dOeDQrcTYyeG1ICmV3YUUycEVOcW9sSk5qVlBBOVRwMWhhQzJvY1l3STZqT1dlVGhaSWUyVDh3K0wzVytncjlMWXhFRnpvNFMrSmEKTWxPT01jUndEeis5bGpYOHpURXdnNHZETU05bUxYTE5WS3gxUXdJREFRQUJBb0lCQUVVMC9CeXovd1JmSWIxSApJa1FXc0UvL0pNbkMycU5RVmIyWkxTanlEM2ZZZ0xCZ0ZkTGQwMGlrMld6YWZibVp1MVljVUJlS3p0SXROcTFsCldKcDloN3BMQk8va1BMbzZvZ2YvT1FXb09QUzV2V29QeVgraG9hcU5QR3JwUW5XTEVsa2R1ZU0wN1Q5eWlydHAKSVRMY1RHdVNzUU9qL1hiVzVMM0x1NWtZTWRNeUN0SHlrUEVMeEpUN2crWkM3UmJuVDdweVN6SWpVRXNlMmtTVAozaFR4cytkekhJc3RqZHNzZWtQSlVBWDBIWi94UFpXek1TMWFvL1V6Sk5jMHpLbDFqRDJXV0k3Q0VodWpjL3QvCjQ0eExYQ0VBQUZTVUxMa29wLytkTDZldjc2SklIVlIxd3VvTHpMT0dVbDJ6UEdzUU9WLzVndTJmQ0xJOG11TlYKMXVtQ213RUNnWUVBM0xrQVZtZ0lteERPRzJNb291UGJVTkZ4NWNUcWZsYjRZK2dUSmhMbGw2T3ZsVno2Y0xnSwp3SWlIcnMwTWZ4YTYva01aMFZveDNxK0lsMk5LaDR2TXg3ZVY5STNnMGJ3RGhMUkxRQ3J1M29XRkpBZkZoa3FxCi9wUk41NElLU0orNTZhWEJwSkpKbU12a013RW1KL0RheklhMkF2cUlrRE9wWWdINkY3NXJYMEVDZ1lFQThJbE0KUy9zcmFFSXRQNUU2TjZwd1RZU0NrK2tLdndZRm15bXIxc2E0U29oNkJGbGpxSC81ZW5FV1BHcnRlTTVHMDFhYQpTTkRLVlRicHVRcmxSK3pTczRJUHBJWTk0Q2ZLTnNSZWpnOTlzRXBWSGZtU2o1UG9SVHQzditldDBOVXIvREE5Cmd5MHpBMG5qUW9KMUpOTVNEQnBYUnpReDFJNmlEdkNKai9rVDk0TUNnWUFJZ1FRM1VBak0yS2ZvUERqTGxkWFUKVmsxNkdjMGpFdnk4OUtzUU0zZ3ZFSHBxV2N1NFhnN2ovaDZrS0hoTHlUZHBKbkt2TXpkcXFmNnNQb0lYbU5aSgo5NVBLZVZEcEk4Sks4WnRZbkk3WmVmRjRRdWhrVlNvalp0bGRpeEFVWGpzT2VubHNlc3BsSGEzc0hTWTRNYnBzCldPQllXd2k1N1pPZ0dBMW5yc2w2UVFLQmdRQ0xEUFA4WUtEQlRyQlZ0U0RRbVVqK3B3SE5lOFRvbFJTY2xFUncKanNSdTRlS1hyUTA5bFcybGFNYVArc2g1TTlZaHlraTZtMmk4UmxocXptK3BXckNiY1M2Vno3enBYbGM1dmQ5agpoSFVHZXBJbUYrYXY5Yk1xZ3F4QlZpOVhNRVNUTDFnQUF4c2daWkJwSEgyWDRpVG10anVLUUJRbWFxWW91TWp0ClgvSTQvUUtCZ0RONFk1TDZDR1JwV3A5V3hCR3AwbVdwcndIYnFJbEV1bWEwSGJPaWJ5TldHOGtKUk5ubGM4aXYKamY4a3U3SDhGbjQxRTlNZkl5SXBEM20wczdDZ3d4Nzg2dnluRkZhS0pxRzQwQjZHcVBUdDZUSFd1Y3hiOEhBZQpHdlcydTQyT25jUXVYdlFEV0EzQ3J3SVlMQ3l4YlJyS040eGdleGFOakcwRERsV0RrM2NCCi0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg=="
)

func TestSaveAndGetPrivateKey(t *testing.T) {
	db, err := kv.New("./data/db", *zap.L(), &kv.Options{InMemory: true})
	require.NoError(t, err)
	defer db.Close()

	operatorStorage := OperatorStorage{
		prefix: []byte("operator-"),
		db:     db,
		logger: nil,
	}

	KeyByte, err := base64.StdEncoding.DecodeString(skPem) // passing keys format should be in base64
	require.NoError(t, err)
	require.NoError(t, operatorStorage.savePrivateKey(string(KeyByte)))
	sk, err := operatorStorage.GetPrivateKey()
	require.NoError(t, err)
	operatorPublicKey, err := rsaencryption.ExtractPublicKey(sk)
	require.NoError(t, err)
	require.Equal(t, pkPem, operatorPublicKey)
}

func TestSetupPrivateKey(t *testing.T) {
	tests := []struct {
		name      string
		existKey  string
		passedKey string
	}{
		{
			name:      "key not exist passing nothing", // expected - generate new key
			existKey:  "",
			passedKey: "",
		},
		{
			name:      "key not exist passing key in env", // expected - set the passed key
			existKey:  "",
			passedKey: skPem2,
		},
		{
			name:      "key exist passing key in env", // expected - override current key with the passed one
			existKey:  skPem,
			passedKey: skPem2,
		},
		{
			name:      "key exist passing nothing", // expected - do nothing
			existKey:  skPem,
			passedKey: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := kv.New("./data/db", *zap.L(), &kv.Options{InMemory: true})
			require.NoError(t, err)
			defer db.Close()

			operatorStorage := OperatorStorage{
				prefix: []byte("operator-"),
				db:     db,
				logger: zap.L(),
			}

			if test.existKey != "" { // mock exist key
				existKeyByte, err := base64.StdEncoding.DecodeString(test.existKey) // passing keys format should be in base64
				require.NoError(t, err)
				require.NoError(t, operatorStorage.savePrivateKey(string(existKeyByte)))
			}
			require.NoError(t, operatorStorage.SetupPrivateKey(test.passedKey))
			sk, err := operatorStorage.GetPrivateKey()
			require.NoError(t, err)

			if test.existKey == "" && test.passedKey == "" { // new key generated
				require.NotNil(t, params.SsvConfig().OperatorPublicKey)
			}
			if test.passedKey != "" { // passed key set
				passedKeyByte, err := base64.StdEncoding.DecodeString(test.passedKey)
				require.NoError(t, err)
				require.Equal(t, string(passedKeyByte), string(rsaencryption.PrivateKeyToByte(sk)))
			}
		})
	}
}
