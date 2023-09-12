package types

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strings"
)

var (
	msg           = "Test.\n"
	encodedSig    = "2ffae3f3e130287b3a1dcb320e46f52e8f3f7969b646932273a7e3a6f2a182ea02d42875a7ffa4a148aa311f9e4b562e4e13a2223fb15f4e5bf5f2b206d9451b"
	rawSig, _     = hex.DecodeString(encodedSig)
	digest        = "8283daccd02d2b2797da53446d367e39dc0fb1615bcae4274460f44455bd9822"
	rawDigest, _  = hex.DecodeString(digest)
	rsaPrivateKey = parseKey(testingKey(`-----BEGIN RSA TESTING KEY-----
MIIBOgIBAAJBALKZD0nEffqM1ACuak0bijtqE2QrI/KLADv7l3kK3ppMyCuLKoF0
fd7Ai2KW5ToIwzFofvJcS/STa6HA5gQenRUCAwEAAQJBAIq9amn00aS0h/CrjXqu
/ThglAXJmZhOMPVn4eiu7/ROixi9sex436MaVeMqSNf7Ex9a8fRNfWss7Sqd9eWu
RTUCIQDasvGASLqmjeffBNLTXV2A5g4t+kLVCpsEIZAycV5GswIhANEPLmax0ME/
EO+ZJ79TJKN5yiGBRsv5yvx5UiHxajEXAiAhAol5N4EUyq6I9w1rYdhPMGpLfk7A
IU2snfRJ6Nq2CQIgFrPsWRCkV+gOYcajD17rEqmuLrdIRexpg8N1DOSXoJ8CIGlS
tAboUGBxTDq3ZroNism3DaMIbKPyYrAqhKov1h5V
-----END RSA TESTING KEY-----`))
)

func testingKey(s string) string { return strings.ReplaceAll(s, "TESTING KEY", "PRIVATE KEY") }

func parseKey(s string) *rsa.PrivateKey {
	p, _ := pem.Decode([]byte(s))
	k, err := x509.ParsePKCS1PrivateKey(p.Bytes)
	if err != nil {
		panic(err)
	}
	return k
}
