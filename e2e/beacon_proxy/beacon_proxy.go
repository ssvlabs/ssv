package beaconproxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type BeaconProxy struct {
	addr   string
	remote *url.URL
	proxy  *httputil.ReverseProxy
}

func New(listenAddr, remoteAddr string) (*BeaconProxy, error) {
	remote, err := url.Parse(remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote beacon address: %w", err)
	}
	return &BeaconProxy{
		addr:   listenAddr,
		remote: remote,
	}, nil
}

func (b *BeaconProxy) Run() error {
	b.proxy = httputil.NewSingleHostReverseProxy(b.remote)
	server := http.Server{
		Addr:         b.addr,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", b.intercept)
	return server.ListenAndServe()
}

func (b *BeaconProxy) intercept(w http.ResponseWriter, r *http.Request) {
	r.Host = b.remote.Host
	b.proxy.ServeHTTP(w, r)
}
