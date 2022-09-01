package grpcpool

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

var (
	ErrStringSplit    = errors.New("err string split")
	ErrNotFoundClient = errors.New("not found grpc conn")
	ErrConnShutdown   = errors.New("grpc conn shutdown")

	defaultClientPoolCap    = 5
	defaultDialTimeout      = time.Second * 5
	defaultKeepAlive        = time.Second * 30
	defaultKeepAliveTimeout = time.Second * 10
)

type ClientOption struct {
	DialTimeout      time.Duration
	KeepAlive        time.Duration
	KeepAliveTimeout time.Duration
	ClientPoolSize   int
}

func NewDefaultClientOption() *ClientOption {
	return &ClientOption{
		DialTimeout:      defaultDialTimeout,
		KeepAlive:        defaultKeepAlive,
		KeepAliveTimeout: defaultKeepAliveTimeout,
	}
}

type ClientPool struct {
	option   *ClientOption
	capacity int64
	next     int64
	target   string

	sync.Mutex

	conns []*grpc.ClientConn
}

func NewClient(target string, option *ClientOption) *ClientPool {
	if option.ClientPoolSize <= 0 {
		option.ClientPoolSize = defaultClientPoolCap
	}

	return &ClientPool{
		target:   target,
		conns:    make([]*grpc.ClientConn, option.ClientPoolSize),
		capacity: int64(option.ClientPoolSize),
		option:   option,
	}
}

func (cc *ClientPool) init() {
	for idx := range cc.conns {
		conn, _ := cc.connect()
		cc.conns[idx] = conn
	}
}

func (cc *ClientPool) checkState(conn *grpc.ClientConn) error {
	state := conn.GetState()
	switch state {
	case connectivity.TransientFailure, connectivity.Shutdown:
		return ErrConnShutdown
	}

	return nil
}

func (cc *ClientPool) getConn() (*grpc.ClientConn, error) {
	var (
		idx  int64
		next int64

		err error
	)

	next = atomic.AddInt64(&cc.next, 1)
	idx = next % cc.capacity
	conn := cc.conns[idx]
	if conn != nil && cc.checkState(conn) == nil {
		return conn, nil
	}

	if conn != nil {
		_ = conn.Close()
	}

	cc.Lock()
	defer cc.Unlock()

	conn, err = cc.connect()
	if err != nil {
		log.Printf("[err] connect: %v", err)
		return nil, err
	}

	cc.conns[idx] = conn
	return conn, nil
}

func (cc *ClientPool) connect() (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(cc.target,
		grpc.WithInsecure(),
		// grpc.WithBlock(),
		grpc.WithTimeout(cc.option.DialTimeout),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    cc.option.KeepAlive,
			Timeout: cc.option.KeepAliveTimeout},
		),
	)
	if err != nil {
		log.Printf("[err] connect: %v", err)
		return nil, err
	}

	return conn, nil
}

func (cc *ClientPool) Close() {
	cc.Lock()
	defer cc.Unlock()

	for _, conn := range cc.conns {
		if conn == nil {
			continue
		}

		_ = conn.Close()
	}
}

type TargetServiceNames struct {
	m map[string][]string
}

func NewTargetServiceNames() *TargetServiceNames {
	return &TargetServiceNames{
		m: make(map[string][]string),
	}
}

func (h *TargetServiceNames) Set(target string, serviceNames ...string) {
	if len(serviceNames) == 0 {
		return
	}

	sourceServNames := h.m[target]
	for _, sn := range serviceNames {
		sourceServNames = append(sourceServNames, sn)
	}

	h.m[target] = sourceServNames
}

func (h *TargetServiceNames) List() map[string][]string {
	return h.m
}

func (h *TargetServiceNames) Cover(m map[string][]string) {
	h.m = m
}

func (h *TargetServiceNames) len() int {
	return len(h.m)
}

type ServiceClientPool struct {
	clients   map[string]*ClientPool
	clientCap int
	option    *ClientOption
}

func NewServiceClientPool(option *ClientOption) *ServiceClientPool {
	return &ServiceClientPool{
		clientCap: option.ClientPoolSize,
		option:    option,
	}
}

func (sc *ServiceClientPool) Init(m TargetServiceNames) {

	var (
		clients = make(map[string]*ClientPool, m.len())
	)

	for target, servNames := range m.List() {
		cc := NewClient(target, sc.option)
		for _, serv := range servNames {
			clients[serv] = cc
		}
	}

	sc.clients = clients
}

func (sc *ServiceClientPool) GetAllClients() map[string]*ClientPool {
	return sc.clients
}

func (sc *ServiceClientPool) GetClientWithFullMethod(fullMethod string) (*grpc.ClientConn, error) {
	name := sc.ExtractServiceName(fullMethod)
	return sc.GetClient(name)
}

func (sc *ServiceClientPool) GetClient(name string) (*grpc.ClientConn, error) {
	cc, ok := sc.clients[name]
	if !ok {
		log.Print("[err] GetClient: not found client")
		return nil, ErrNotFoundClient
	}

	return cc.getConn()
}

func (sc *ServiceClientPool) CloseWithFullMethod(fullMethod string) {
	name := sc.ExtractServiceName(fullMethod)
	sc.Close(name)
}

func (sc *ServiceClientPool) Close(name string) {
	cc, ok := sc.clients[name]
	if !ok {
		return
	}

	cc.Close()
}

func (sc *ServiceClientPool) CloseAll() {
	for _, client := range sc.clients {
		client.Close()
	}
}

func (sc *ServiceClientPool) ExtractServiceName(fullMethod string) string {
	sm := strings.Split(fullMethod, "/")
	if len(sm) != 3 {
		return ""
	}
	return "/" + sm[1]
}

func (sc *ServiceClientPool) GetMethodURI(fullMethod string) string {
	sm := strings.Split(fullMethod, "/")
	if len(sm) != 3 {
		return ""
	}
	return fmt.Sprintf("/%s/%s", sm[1], sm[2])
}

func (sc *ServiceClientPool) Invoke(ctx context.Context, fullMethod string, headers map[string]string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	var (
		md metadata.MD
	)

	name := sc.ExtractServiceName(fullMethod)
	conn, err := sc.GetClient(name)
	if err != nil {
		log.Printf("[err] GetClient: %v", err)
		return err
	}

	md, flag := metadata.FromOutgoingContext(ctx)
	if flag == true {
		md = md.Copy()
	} else {
		md = metadata.MD{}
	}

	for key, value := range headers {
		md.Set(key, value)
	}

	ctx = metadata.NewOutgoingContext(ctx, md)
	methodURI := sc.GetMethodURI(fullMethod)
	err = conn.Invoke(ctx, methodURI, args, reply, opts...)
	if err != nil {
		log.Printf("[err] Invoke: %v", err)
		return err
	}

	return nil
}
