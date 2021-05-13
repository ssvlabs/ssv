package cli

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// generateOperatorKeysCmd is the command to generate operator private/public keys
var generateOperatorKeysCmd = &cobra.Command{
	Use:   "generate-operator-keys",
	Short: "generates ssv operator keys",
	Run: func(cmd *cobra.Command, args []string) {
		logger := logex.Build(RootCmd.Short, zapcore.DebugLevel)

		// generate random private key (secret)
		sk, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			logger.Error("Failed to generate operator private key", zap.Error(err))
			return
		}
		// retrieve public key from the newly generated secret
		pk := &sk.PublicKey

		// convert to bytes
		skPem := pem.EncodeToMemory(
			&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(sk),
			},
		)
		pkBytes, err := x509.MarshalPKIXPublicKey(pk)
		if err != nil {
			logger.Error("Failed to marshal public key", zap.Error(err))
			return
		}
		pkPem := pem.EncodeToMemory(
			&pem.Block{
				Type:  "RSA PUBLIC KEY",
				Bytes: pkBytes,
			},
		)

		logger.Info("generated private key (base64)", zap.Any("sk", skPem))
		logger.Info("generated public key (base64)", zap.Any("pk", pkPem))
	},
}

//func convertPemToPrivateKey(logger *zap.Logger) *rsa.PrivateKey {
//	block, _ := pem.Decode([]byte("-----BEGIN RSA PRIVATE KEY-----\nMIICXQIBAAKBgQDMQ6q8kgHF2LEhGmnlaoKGIxJx2Bh/fb0vFTq0DPeeJgCNeYPJ\n2ClQ316p+EMBjXAY7bC81Y2u/FGubuAzfH0JC9ENnxnlBopnSYcCzNuxRe/MwIrs\nrLc1krHNpgFMYrTmsPjtB6cmA454SfdYO1lvcxUxCOdNQZc3PwxK8EHyoQIDAQAB\nAoGATiTU/K8e3oG3weJJAOtuY8KnG8aAGMYRyiFlA9yyHl6Ld5Q1RtLbe4T4wi2n\n9MAXUnIcWyGXwonk9caVHx1Q96TysDgkoALEsmq0so/bSgQGC53aUwrZEVdiwph4\nT0F+hLl8ov5eynHuiYMHFRgJdhHoco4NNKzCqZ0MX+KBVsECQQD/e0Mn6BLhPBOK\no0fCkwYP20CsRPeU35gvUNEfak8FWz1fbFaf7n3d40j6lyABVQx2EjEDsZGhN6jl\n1dq/awhLAkEAzK3LVdFZgOh+t8rDU1T77DyTjexCJpXQ2E89mHL4MzlAMLKwRL31\nQAr3QiMJWTxHQ0aW6EvCjLGqc3Q5JY31QwJBAMM2z4jFtu9t9UyhGSsfJqmlEhTQ\nGhIii+nTqgeENt9T6WBpqwNHu9t5WYFJSsZZ00zA97znyOxUWHVOZHiRc2MCQQCP\nTof1uDSQmzhN+vuzluckSm2NiwPt/CtTqHeaC7VYOBeHgTUFjHLwujzQ47Mh9aB3\nrC7wykqXM7YCTDfO4Yv9AkBP2igHYwm9Ly88sg4EOt+FvVhqzwjzldfotDFbb6Ly\ngECRZsqYkgW3Xzu74ZOs+L+jC3fmI4zYNSx3M4Vufy55\n-----END RSA PRIVATE KEY-----\n"))
//	enc:= x509.IsEncryptedPEMBlock(block)
//	b := block.Bytes
//	if enc{
//		var err error
//		b, err = x509.DecryptPEMBlock(block, nil)
//		if err != nil {
//			logger.Error("Failed to decrypt operator private key", zap.Error(err))
//		}
//	}
//	parsedSk, err := x509.ParsePKCS1PrivateKey(b)
//	if err != nil {
//		logger.Error("Failed to parse operator private key", zap.Error(err))
//		return nil
//	}
//	return parsedSk
//}
//
//func decrypteHash(logger *zap.Logger, sk *rsa.PrivateKey, hash []byte) {
//	dectyptedKey, err := rsa.DecryptPKCS1v15(rand.Reader, sk, hash)
//	if err != nil {
//		logger.Error("Failed to decrypt key", zap.Error(err))
//	}
//	logger.Info("decrypted key", zap.Any("key", string(dectyptedKey)))
//}

func init() {
	RootCmd.AddCommand(generateOperatorKeysCmd)
}
