package ingress

import (
	"fmt"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/caddyserver/caddy/v2/modules/caddytls"
	"github.com/go-logr/zapr"
	"github.com/kohcojlb/caddy-ingress-proxy/controller"
	"go.uber.org/zap"
	"k8s.io/klog/v2"
	"net/http"
)

type Handler struct {
	KubeconfigPath string `json:"kubeconfig"`
	IngressAddr    string `json:"ingress_addr"`

	ctrl   *controller.Controller
	proxy  reverseproxy.Handler
	tls    *caddytls.TLS
	logger *zap.SugaredLogger
	routes map[string]bool
}

func (h *Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "http.handlers.kube_ingress",
		New: func() caddy.Module {
			return new(Handler)
		},
	}
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request, handler caddyhttp.Handler) error {
	if _, ok := h.routes[request.Host]; ok {
		return h.proxy.ServeHTTP(writer, request, handler)
	}
	return handler.ServeHTTP(writer, request)
}

func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger().Sugar()

	klog.SetLogger(zapr.NewLogger(ctx.Logger()))

	tlsApp, err := ctx.App("tls")
	if err != nil {
		return err
	}
	h.tls = tlsApp.(*caddytls.TLS)

	h.routes = make(map[string]bool)
	h.proxy.Upstreams = reverseproxy.UpstreamPool{
		{
			Dial: h.IngressAddr,
		},
	}
	err = h.proxy.Provision(ctx)
	if err != nil {
		h.proxy.Cleanup()
		return fmt.Errorf("provision reverse_proxy: %w", err)
	}

	h.ctrl, err = controller.New(h.KubeconfigPath, h)
	if err != nil {
		return err
	}
	h.ctrl.Start(ctx)

	return nil
}

func (h *Handler) AddRoute(route string) {
	h.logger.Infow("add route", "route", route)

	err := h.tls.Manage([]string{route})
	if err != nil {
		h.logger.Errorf("manage certificate for %s: %s", route, err)
	}

	h.routes[route] = true
	h.logger.Infof("Added ingress for %s", route)
}

func (h *Handler) RemoveRoute(route string) {
	h.logger.Infow("remove route", "route", route)
	delete(h.routes, route)
	h.logger.Infof("Removed ingress for %s", route)
}

func (h *Handler) Cleanup() error {
	return h.proxy.Cleanup()
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var handler Handler
	if h.Next() {
		for nesting := h.Nesting(); h.NextBlock(nesting); {
			switch h.Val() {
			case "kubeconfig":
				h.Args(&handler.KubeconfigPath)
			case "ingress_addr":
				h.Args(&handler.IngressAddr)
			}
		}
	}

	if handler.KubeconfigPath == "" {
		return nil, h.Err("kubeconfig not defined")
	}
	if handler.IngressAddr == "" {
		return nil, h.Err("ingress_addr not defined")
	}

	return &handler, nil
}

func init() {
	caddy.RegisterModule(new(Handler))
	httpcaddyfile.RegisterHandlerDirective("kube_ingress", parseCaddyfile)
}
