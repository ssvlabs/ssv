package server

import (
	"encoding/json"
	"fmt"
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/operator/keys"
)

type Server struct {
	logger         *zap.Logger
	router         *router.Router
	keystorePasswd string
}

func New(
	logger *zap.Logger,
	operatorPrivKey keys.OperatorPrivateKey,
	remoteSigner RemoteSigner,
	keystorePasswd string,
) *Server {
	r := router.New()

	server := &Server{
		logger:          logger,
		operatorPrivKey: operatorPrivKey,
		remoteSigner:    remoteSigner,
		router:          r,
		keystorePasswd:  keystorePasswd,
	}

	r.GET("/signer/v1/get_pubkeys", server.handleGetPubkeys)
	r.POST("/signer/v1/request_signature", server.handleRequestSignature)
	// TODO generate_proxy_key is not implemented at the moment
	//r.POST("/signer/v1/generate_proxy_key", server.handleListValidators)

	r.GET("/status", server.handleStatus)

	return server
}

func (r *Server) Handler() func(ctx *fasthttp.RequestCtx) {
	return r.router.Handler
}

func (r *Server) handleGetPubkeys(ctx *fasthttp.RequestCtx) {
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

	statuses, err := r.remoteSigner.DeleteKeystore(ctx, req.PublicKeys)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to remove share from Web3Signer: %w", err))
		return
	}

	for i, status := range statuses {
		if status != StatusDeleted {
			r.logger.Warn("unexpected status",
				zap.String("status", string(status)),
				zap.String("share_pubkey", req.PublicKeys[i]),
			)
		}
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

func (r *Server) handleStatus(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeString(ctx, "OK")
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
