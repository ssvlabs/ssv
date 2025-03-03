package server

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fasthttp/router"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/operator/keystore"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type Server struct {
	logger          *zap.Logger
	operatorPrivKey keys.OperatorPrivateKey
	web3Signer      *web3signer.Web3Signer
	router          *router.Router
	keystorePasswd  string
}

func New(
	logger *zap.Logger,
	operatorPrivKey keys.OperatorPrivateKey,
	web3Signer *web3signer.Web3Signer,
	keystorePasswd string,
) *Server {
	r := router.New()

	server := &Server{
		logger:          logger,
		operatorPrivKey: operatorPrivKey,
		web3Signer:      web3Signer,
		router:          r,
		keystorePasswd:  keystorePasswd,
	}

	r.POST("/v1/validators/add", server.handleAddValidator)
	r.POST("/v1/validators/remove", server.handleRemoveValidator)
	r.POST("/v1/validators/sign/{identifier}", server.handleSignValidator)

	r.GET("/v1/operator/identity", server.handleOperatorIdentity)
	r.POST("/v1/operator/sign", server.handleSignOperator)

	return server
}

func (r *Server) Handler() func(ctx *fasthttp.RequestCtx) {
	return r.router.Handler
}

type Status = web3signer.Status

type AddValidatorRequest struct {
	ShareKeys []ShareKeys `json:"share_keys"`
}

type ShareKeys struct {
	EncryptedPrivKey string `json:"encrypted_private_key"`
	PublicKey        string `json:"public_key"`
}

type AddValidatorResponse struct {
	Statuses []Status
}

func (r *Server) handleAddValidator(ctx *fasthttp.RequestCtx) {
	body := ctx.PostBody()
	if len(body) == 0 {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeString(ctx, "request body is empty")
		return
	}

	var req AddValidatorRequest
	if err := json.Unmarshal(body, &req); err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	var encShareKeystores, shareKeystorePasswords []string

	for _, share := range req.ShareKeys {
		encPrivKey, err := hex.DecodeString(strings.TrimPrefix(share.EncryptedPrivKey, "0x"))
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			r.writeErr(ctx, fmt.Errorf("failed to decode share.EncryptedPrivKey as hex: %w", err))
			return
		}

		pubKey, err := hex.DecodeString(strings.TrimPrefix(share.PublicKey, "0x"))
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			r.writeErr(ctx, fmt.Errorf("failed to decode share.PublicKey as hex: %w", err))
			return
		}

		sharePrivKeyHex, err := r.operatorPrivKey.Decrypt(encPrivKey)
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusUnauthorized)
			r.writeErr(ctx, fmt.Errorf("failed to decrypt share: %w", err))
			return
		}

		sharePrivKey, err := hex.DecodeString(strings.TrimPrefix(string(sharePrivKeyHex), "0x"))
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			r.writeErr(ctx, fmt.Errorf("failed to decode share private key from hex %s: %w", string(sharePrivKeyHex), err))
			return
		}

		sharePrivBLS := &bls.SecretKey{}
		if err = sharePrivBLS.Deserialize(sharePrivKey); err != nil {
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			r.writeErr(ctx, fmt.Errorf("failed to parse share private key as BLS %s: %w", string(sharePrivKeyHex), err))
			return
		}

		if !bytes.Equal(sharePrivBLS.GetPublicKey().Serialize(), pubKey) {
			ctx.SetStatusCode(fasthttp.StatusUnauthorized)
			r.writeErr(ctx, fmt.Errorf("derived public key does not match expected public key"))
			return
		}

		shareKeystore, err := keystore.GenerateShareKeystore(sharePrivKey, pubKey, r.keystorePasswd)
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			r.writeErr(ctx, fmt.Errorf("failed to generate share keystore: %w", err))
			return
		}

		keystoreJSON, err := json.Marshal(shareKeystore)
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			r.writeErr(ctx, fmt.Errorf("marshal share keystore: %w", err))
			return
		}

		encShareKeystores = append(encShareKeystores, string(keystoreJSON))
		shareKeystorePasswords = append(shareKeystorePasswords, r.keystorePasswd)
	}

	statuses, err := r.web3Signer.ImportKeystore(encShareKeystores, shareKeystorePasswords)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to import share to Web3Signer: %w", err))
		return
	}

	resp := AddValidatorResponse{
		Statuses: statuses,
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to marshal statuses: %w", err))
		return
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeBytes(ctx, respJSON)
}

type RemoveValidatorRequest struct {
	PublicKeys []string `json:"public_keys"`
}

type RemoveValidatorResponse struct {
	Statuses []Status `json:"statuses"`
}

func (r *Server) handleRemoveValidator(ctx *fasthttp.RequestCtx) {
	body := ctx.PostBody()
	if len(body) == 0 {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeString(ctx, "request body is empty")
		return
	}

	var req RemoveValidatorRequest
	if err := json.Unmarshal(body, &req); err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	statuses, err := r.web3Signer.DeleteKeystore(req.PublicKeys)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to remove share from Web3Signer: %w", err))
		return
	}

	resp := RemoveValidatorResponse{
		Statuses: statuses,
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to marshal statuses: %w", err))
		return
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeBytes(ctx, respJSON)
}

func (r *Server) handleSignValidator(ctx *fasthttp.RequestCtx) {
	var req web3signer.SignRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("invalid request body: %w", err))
		return
	}

	sharePubKeyHex, ok := ctx.UserValue("identifier").(string)
	if !ok {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("invalid share public key"))
		return
	}

	sharePubKey, err := hex.DecodeString(strings.TrimPrefix(sharePubKeyHex, "0x"))
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("malformed share public key"))
		return
	}

	sig, err := r.web3Signer.Sign(sharePubKey, req)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to sign with Web3Signer: %w", err))
		return
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeBytes(ctx, sig)
}

func (r *Server) handleOperatorIdentity(ctx *fasthttp.RequestCtx) {
	pubKeyB64, err := r.operatorPrivKey.Public().Base64()
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to get public key base64: %w", err))
		return
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeBytes(ctx, pubKeyB64)
}

func (r *Server) handleSignOperator(ctx *fasthttp.RequestCtx) {
	payload := ctx.PostBody()

	signature, err := r.operatorPrivKey.Sign(payload)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to sign message: %w", err))
		return
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeBytes(ctx, signature)
}

func (r *Server) writeString(ctx *fasthttp.RequestCtx, s string) {
	if _, writeErr := ctx.WriteString(s); writeErr != nil {
		r.logger.Error("failed to write response", zap.Error(writeErr))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (r *Server) writeBytes(ctx *fasthttp.RequestCtx, b []byte) {
	if _, writeErr := ctx.Write(b); writeErr != nil {
		r.logger.Error("failed to write response", zap.Error(writeErr))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (r *Server) writeErr(ctx *fasthttp.RequestCtx, err error) {
	if _, writeErr := ctx.WriteString(err.Error()); writeErr != nil {
		r.logger.Error("failed to write response", zap.Error(writeErr))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}
