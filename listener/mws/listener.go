package mws

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/dolfly/core/limiter"
	"github.com/dolfly/core/listener"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	admission "github.com/dolfly/x/admission/wrapper"
	xctx "github.com/dolfly/x/ctx"
	xnet "github.com/dolfly/x/internal/net"
	xhttp "github.com/dolfly/x/internal/net/http"
	"github.com/dolfly/x/internal/net/proxyproto"
	"github.com/dolfly/x/internal/util/mux"
	xtls "github.com/dolfly/x/internal/util/tls"
	ws_util "github.com/dolfly/x/internal/util/ws"
	climiter "github.com/dolfly/x/limiter/conn/wrapper"
	limiter_wrapper "github.com/dolfly/x/limiter/traffic/wrapper"
	metrics "github.com/dolfly/x/metrics/wrapper"
	stats "github.com/dolfly/x/observer/stats/wrapper"
	"github.com/dolfly/x/registry"
	"github.com/gorilla/websocket"
)

func init() {
	registry.ListenerRegistry().Register("mws", NewListener)
	registry.ListenerRegistry().Register("mwss", NewTLSListener)
}

type mwsListener struct {
	addr       net.Addr
	upgrader   *websocket.Upgrader
	srv        *http.Server
	cqueue     chan net.Conn
	errChan    chan error
	tlsEnabled bool
	log        logger.Logger
	md         metadata
	options    listener.Options
}

func NewListener(opts ...listener.Option) listener.Listener {
	options := listener.Options{}
	for _, opt := range opts {
		opt(&options)
	}
	return &mwsListener{
		log:     options.Logger,
		options: options,
	}
}

func NewTLSListener(opts ...listener.Option) listener.Listener {
	options := listener.Options{}
	for _, opt := range opts {
		opt(&options)
	}
	return &mwsListener{
		tlsEnabled: true,
		log:        options.Logger,
		options:    options,
	}
}

func (l *mwsListener) Init(md md.Metadata) (err error) {
	if err = l.parseMetadata(md); err != nil {
		return
	}

	l.upgrader = &websocket.Upgrader{
		HandshakeTimeout:  l.md.handshakeTimeout,
		ReadBufferSize:    l.md.readBufferSize,
		WriteBufferSize:   l.md.writeBufferSize,
		EnableCompression: l.md.enableCompression,
		CheckOrigin:       func(r *http.Request) bool { return true },
	}

	path := l.md.path
	if path == "" {
		path = defaultPath
	}
	mux := http.NewServeMux()
	mux.Handle(path, http.HandlerFunc(l.upgrade))
	l.srv = &http.Server{
		Addr:              l.options.Addr,
		Handler:           mux,
		ReadHeaderTimeout: l.md.readHeaderTimeout,
	}

	l.cqueue = make(chan net.Conn, l.md.backlog)
	l.errChan = make(chan error, 1)

	network := "tcp"
	if xnet.IsIPv4(l.options.Addr) {
		network = "tcp4"
	}

	lc := net.ListenConfig{}
	if l.md.mptcp {
		lc.SetMultipathTCP(true)
		l.log.Debugf("mptcp enabled: %v", lc.MultipathTCP())
	}
	ln, err := lc.Listen(context.Background(), network, l.options.Addr)
	if err != nil {
		return
	}
	ln = proxyproto.WrapListener(l.options.ProxyProtocol, ln, 10*time.Second)
	ln = metrics.WrapListener(l.options.Service, ln)
	ln = stats.WrapListener(ln, l.options.Stats)
	ln = admission.WrapListener(l.options.Admission, ln)
	ln = limiter_wrapper.WrapListener(l.options.Service, ln, l.options.TrafficLimiter)
	ln = climiter.WrapListener(l.options.ConnLimiter, ln)

	if l.tlsEnabled {
		ln = xtls.NewListener(ln, l.options.TLSConfig)
	}

	l.addr = ln.Addr()

	go func() {
		err := l.srv.Serve(ln)
		if err != nil {
			l.errChan <- err
		}
		close(l.errChan)
	}()

	return
}

func (l *mwsListener) Accept() (conn net.Conn, err error) {
	var ok bool
	select {
	case conn = <-l.cqueue:
		conn = limiter_wrapper.WrapConn(
			conn,
			l.options.TrafficLimiter,
			conn.RemoteAddr().String(),
			limiter.ScopeOption(limiter.ScopeConn),
			limiter.ServiceOption(l.options.Service),
			limiter.NetworkOption(conn.LocalAddr().Network()),
			limiter.SrcOption(conn.RemoteAddr().String()),
		)

	case err, ok = <-l.errChan:
		if !ok {
			err = listener.ErrClosed
		}
	}
	return
}

func (l *mwsListener) Close() error {
	return l.srv.Close()
}

func (l *mwsListener) Addr() net.Addr {
	return l.addr
}

func (l *mwsListener) upgrade(w http.ResponseWriter, r *http.Request) {
	clientIP := xhttp.GetClientIP(r)
	cip := ""
	if clientIP != nil {
		cip = clientIP.String()
	}
	log := l.log.WithFields(map[string]any{
		"local":  l.addr.String(),
		"remote": r.RemoteAddr,
		"client": cip,
	})
	if log.IsLevelEnabled(logger.TraceLevel) {
		dump, _ := httputil.DumpRequest(r, false)
		log.Trace(string(dump))
	}

	conn, err := l.upgrader.Upgrade(w, r, l.md.header)
	if err != nil {
		log.Error(err)
		return
	}

	ctx := r.Context()
	if cc, ok := conn.NetConn().(xctx.Context); ok {
		if cv := cc.Context(); cv != nil {
			ctx = cv
		}
	}

	if clientIP != nil {
		ctx = xctx.ContextWithSrcAddr(ctx, &net.TCPAddr{IP: clientIP})
	}

	l.mux(ws_util.ContextConn(ctx, conn), log)
}

func (l *mwsListener) mux(conn net.Conn, log logger.Logger) {
	defer conn.Close()

	session, err := mux.ServerSession(conn, l.md.muxCfg)
	if err != nil {
		log.Error(err)
		return
	}
	defer session.Close()

	for {
		stream, err := session.Accept()
		if err != nil {
			log.Error("accept stream: ", err)
			return
		}

		select {
		case l.cqueue <- stream:
		default:
			stream.Close()
			log.Warnf("connection queue is full, client %s discarded", stream.RemoteAddr())
		}
	}
}
