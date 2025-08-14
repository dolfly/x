package ingress

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/dolfly/core/ingress"
	"github.com/dolfly/core/logger"
	"github.com/dolfly/x/internal/loader"
	xlogger "github.com/dolfly/x/logger"
)

type options struct {
	rules       []*ingress.Rule
	fileLoader  loader.Loader
	redisLoader loader.Loader
	httpLoader  loader.Loader
	period      time.Duration
	logger      logger.Logger
}

type Option func(opts *options)

func RulesOption(rules []*ingress.Rule) Option {
	return func(opts *options) {
		opts.rules = rules
	}
}

func ReloadPeriodOption(period time.Duration) Option {
	return func(opts *options) {
		opts.period = period
	}
}

func FileLoaderOption(fileLoader loader.Loader) Option {
	return func(opts *options) {
		opts.fileLoader = fileLoader
	}
}

func RedisLoaderOption(redisLoader loader.Loader) Option {
	return func(opts *options) {
		opts.redisLoader = redisLoader
	}
}

func HTTPLoaderOption(httpLoader loader.Loader) Option {
	return func(opts *options) {
		opts.httpLoader = httpLoader
	}
}

func LoggerOption(logger logger.Logger) Option {
	return func(opts *options) {
		opts.logger = logger
	}
}

type localIngress struct {
	rules      map[string]*ingress.Rule
	options    options
	logger     logger.Logger
	mu         sync.RWMutex
	cancelFunc context.CancelFunc
}

// NewIngress creates and initializes a new Ingress.
func NewIngress(opts ...Option) ingress.Ingress {
	var options options
	for _, opt := range opts {
		opt(&options)
	}

	ctx, cancel := context.WithCancel(context.TODO())

	ing := &localIngress{
		rules:      make(map[string]*ingress.Rule),
		cancelFunc: cancel,
		options:    options,
		logger:     options.logger,
	}
	if ing.logger == nil {
		ing.logger = xlogger.Nop()
	}

	go ing.periodReload(ctx)

	return ing
}

func (ing *localIngress) periodReload(ctx context.Context) error {
	if err := ing.reload(ctx); err != nil {
		ing.logger.Warnf("reload: %v", err)
	}

	period := ing.options.period
	if period <= 0 {
		return nil
	}
	if period < time.Second {
		period = time.Second
	}

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := ing.reload(ctx); err != nil {
				ing.logger.Warnf("reload: %v", err)
				// return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (ing *localIngress) reload(ctx context.Context) error {
	rules := make(map[string]*ingress.Rule)

	fn := func(rule *ingress.Rule) {
		if rule.Hostname == "" || rule.Endpoint == "" {
			return
		}
		host := rule.Hostname
		if host[0] == '*' {
			host = host[1:]
		}
		rules[host] = rule
	}

	for _, rule := range ing.options.rules {
		fn(rule)
	}

	v, err := ing.load(ctx)
	if err != nil {
		return err
	}
	for _, rule := range v {
		fn(rule)
	}

	ing.logger.Debugf("load items %d", len(rules))

	ing.mu.Lock()
	defer ing.mu.Unlock()

	ing.rules = rules

	return nil
}

func (ing *localIngress) load(ctx context.Context) (rules []*ingress.Rule, err error) {
	if ing.options.fileLoader != nil {
		if lister, ok := ing.options.fileLoader.(loader.Lister); ok {
			list, er := lister.List(ctx)
			if er != nil {
				ing.logger.Warnf("file loader: %v", er)
			}
			for _, s := range list {
				rules = append(rules, ing.parseLine(s))
			}
		} else {
			r, er := ing.options.fileLoader.Load(ctx)
			if er != nil {
				ing.logger.Warnf("file loader: %v", er)
			}
			if v, _ := ing.parseRules(r); v != nil {
				rules = append(rules, v...)
			}
		}
	}
	if ing.options.redisLoader != nil {
		if lister, ok := ing.options.redisLoader.(loader.Lister); ok {
			list, er := lister.List(ctx)
			if er != nil {
				ing.logger.Warnf("redis loader: %v", er)
			}
			for _, v := range list {
				rules = append(rules, ing.parseLine(v))
			}
		} else {
			r, er := ing.options.redisLoader.Load(ctx)
			if er != nil {
				ing.logger.Warnf("redis loader: %v", er)
			}
			v, _ := ing.parseRules(r)
			rules = append(rules, v...)
		}
	}
	if ing.options.httpLoader != nil {
		r, er := ing.options.httpLoader.Load(ctx)
		if er != nil {
			ing.logger.Warnf("http loader: %v", er)
		}
		v, _ := ing.parseRules(r)
		rules = append(rules, v...)
	}

	return
}

func (ing *localIngress) parseRules(r io.Reader) (rules []*ingress.Rule, err error) {
	if r == nil {
		return
	}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if rule := ing.parseLine(scanner.Text()); rule.Hostname != "" {
			rules = append(rules, rule)
		}
	}

	err = scanner.Err()
	return
}

func (ing *localIngress) GetRule(ctx context.Context, host string, opts ...ingress.Option) *ingress.Rule {
	if host == "" || ing == nil {
		return nil
	}

	// try to strip the port
	if v, _, _ := net.SplitHostPort(host); v != "" {
		host = v
	}

	ing.logger.Debugf("ingress: lookup %s", host)
	ep := ing.lookup(host)
	if ep == nil {
		ep = ing.lookup("." + host)
	}
	if ep == nil {
		s := host
		for {
			if index := strings.IndexByte(s, '.'); index > 0 {
				ep = ing.lookup(s[index:])
				s = s[index+1:]
				if ep == nil {
					continue
				}
			}
			break
		}
	}

	if ep != nil {
		ing.logger.Debugf("ingress: %s -> %s:%s", host, ep.Hostname, ep.Endpoint)
	}

	return ep
}

func (ing *localIngress) SetRule(ctx context.Context, rule *ingress.Rule, opts ...ingress.Option) bool {
	return false
}

func (ing *localIngress) lookup(host string) *ingress.Rule {
	if ing == nil {
		return nil
	}

	ing.mu.RLock()
	defer ing.mu.RUnlock()

	return ing.rules[host]
}

func (ing *localIngress) parseLine(s string) (rule *ingress.Rule) {
	line := strings.Replace(s, "\t", " ", -1)
	line = strings.TrimSpace(line)
	if n := strings.IndexByte(line, '#'); n >= 0 {
		line = line[:n]
	}
	var sp []string
	for _, s := range strings.Split(line, " ") {
		if s = strings.TrimSpace(s); s != "" {
			sp = append(sp, s)
		}
	}
	if len(sp) < 2 {
		return // invalid lines are ignored
	}

	return &ingress.Rule{
		Hostname: sp[0],
		Endpoint: sp[1],
	}
}

func (ing *localIngress) Close() error {
	ing.cancelFunc()
	if ing.options.fileLoader != nil {
		ing.options.fileLoader.Close()
	}
	if ing.options.redisLoader != nil {
		ing.options.redisLoader.Close()
	}
	return nil
}
