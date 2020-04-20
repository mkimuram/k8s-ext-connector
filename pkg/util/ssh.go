package util

import (
	"context"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	backoffv4 "github.com/cenkalti/backoff/v4"

	glssh "github.com/gliderlabs/ssh"
	"github.com/golang/glog"
	"golang.org/x/crypto/ssh"
)

const (
	// SSHPort is port number to used for ssh server
	// TODO: change this to variable
	SSHPort = "2022"
)

type Tunnel struct {
	localEndpoint  string
	serverEndpoint string
	remoteEndpoint string
	config         *ssh.ClientConfig
	context        context.Context
	backoff        backoffv4.BackOffContext
	Cancel         context.CancelFunc
}

func NewTunnel(local, server, remote string, config *ssh.ClientConfig) *Tunnel {
	ctx, cf := context.WithCancel(context.Background())
	b := backoffv4.WithContext(backoffv4.NewExponentialBackOff(), ctx)
	return &Tunnel{
		localEndpoint:  local,
		serverEndpoint: server,
		remoteEndpoint: remote,
		config:         config,
		context:        ctx,
		backoff:        b,
		Cancel:         cf,
	}
}

func toTCPAddr(endpoint string, portAny bool) (*net.TCPAddr, error) {
	ip, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, err
	}

	if portAny {
		return &net.TCPAddr{
			IP:   net.ParseIP(ip),
			Port: 0,
		}, nil
	}

	intPort, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}

	return &net.TCPAddr{
		IP:   net.ParseIP(ip),
		Port: intPort,
	}, nil
}

func (t *Tunnel) doForward(lCon net.Conn, sCli *ssh.Client) {
	var wg sync.WaitGroup
	laddr, err := toTCPAddr(t.serverEndpoint, true /* portAny */)
	if err != nil {
		return
	}

	raddr, err := toTCPAddr(t.remoteEndpoint, false /* portAny */)
	if err != nil {
		return
	}

	// Use DialTCP and specify laddr to bind server's local endpoint as a source IP,
	// instead of calling Dial without laddr
	rCon, err := sCli.DialTCP("tcp", laddr, raddr)
	if err != nil {
		glog.Errorf("connecting to remote endopoint %q failed: %v", t.remoteEndpoint, err)
		return
	}
	defer rCon.Close()

	copyCon := func(in, out net.Conn) {
		defer wg.Done()
		select {
		case <-t.context.Done():
			return
		default:
			if _, err := io.Copy(in, out); err != nil {
				glog.Errorf("copying io failed: %v", err)
			}
		}
	}

	wg.Add(1)
	go copyCon(lCon, rCon)
	wg.Add(1)
	go copyCon(rCon, lCon)

	wg.Wait()
}

func (t *Tunnel) Forward() error {
	sCli, err := ssh.Dial("tcp", t.serverEndpoint, t.config)
	if err != nil {
		glog.Errorf("connecting to server endopoint %q failed: %v", t.serverEndpoint, err)
		return err
	}
	defer sCli.Close()

	lnr, err := net.Listen("tcp", t.localEndpoint)
	if err != nil {
		glog.Errorf("listening to local endopoint %q failed: %v", t.localEndpoint, err)
		return err
	}
	defer lnr.Close()

	for {
		select {
		case <-t.context.Done():
			break
		default:

			lCon, err := lnr.Accept()
			if err != nil {
				glog.Errorf("accepting on local endopoint %q failed: %v", t.localEndpoint, err)
				return err
			}
			go t.doForward(lCon, sCli)
		}
	}

	return nil
}

func (t *Tunnel) ForwardNB() {
	go backoffv4.Retry(
		func() error {
			return t.Forward()
		},
		t.backoff)
}

func (t *Tunnel) doRemoteForward(rCon net.Conn) {
	var wg sync.WaitGroup

	lCon, err := net.Dial("tcp", t.localEndpoint)
	if err != nil {
		glog.Errorf("connecting to local endopoint %q failed: %v", t.localEndpoint, err)
		return
	}
	defer lCon.Close()

	copyCon := func(in, out net.Conn) {
		defer wg.Done()
		select {
		case <-t.context.Done():
			return
		default:
			if _, err := io.Copy(in, out); err != nil {
				glog.Errorf("copying io failed: %v", err)
			}
		}
	}

	wg.Add(1)
	go copyCon(rCon, lCon)
	wg.Add(1)
	go copyCon(lCon, rCon)

	wg.Wait()
}

func (t *Tunnel) RemoteForward() error {
	glog.Infof("remote forwarding for local%q:server%q:remote%q is started", t.localEndpoint, t.serverEndpoint, t.remoteEndpoint)

	sCli, err := ssh.Dial("tcp", t.serverEndpoint, t.config)
	if err != nil {
		glog.Errorf("connecting to server endopoint %q failed: %v", t.serverEndpoint, err)
		return err
	}
	defer sCli.Close()

	rlnr, err := sCli.Listen("tcp", t.remoteEndpoint)
	if err != nil {
		glog.Errorf("listening to remote endopoint %q failed: %v", t.remoteEndpoint, err)
		return err
	}
	defer rlnr.Close()

	for {
		select {
		case <-t.context.Done():
			break
		default:
			rCon, err := rlnr.Accept()
			if err != nil {
				glog.Errorf("accepting on remote endopoint %q failed: %v", t.remoteEndpoint, err)
				return err
			}
			t.doRemoteForward(rCon)
		}
	}

	return nil
}

func (t *Tunnel) RemoteForwardNB() {
	go backoffv4.Retry(
		func() error {
			return t.RemoteForward()
		},
		t.backoff)
}

// direct-tcpip data struct as specified in RFC4254, Section 7.2
type localForwardChannelData struct {
	DestAddr string
	DestPort uint32

	OriginAddr string
	OriginPort uint32
}

// DirectTCPIPHandler is a handler for direct-tcpip.
// This is modified from gliderlabs original one so that it can reserve source ip.
func DirectTCPIPHandler(srv *glssh.Server, conn *ssh.ServerConn, newChan ssh.NewChannel, ctx glssh.Context) {
	d := localForwardChannelData{}
	if err := ssh.Unmarshal(newChan.ExtraData(), &d); err != nil {
		newChan.Reject(ssh.ConnectionFailed, "error parsing forward data: "+err.Error())
		return
	}

	if srv.LocalPortForwardingCallback == nil || !srv.LocalPortForwardingCallback(ctx, d.DestAddr, d.DestPort) {
		newChan.Reject(ssh.Prohibited, "port forwarding is disabled")
		return
	}

	dest := net.JoinHostPort(d.DestAddr, strconv.FormatInt(int64(d.DestPort), 10))
	origin := net.JoinHostPort(d.OriginAddr, strconv.FormatInt(int64(d.OriginPort), 10))

	laddr, err := toTCPAddr(origin, false /* portAny */)
	if err != nil {
		newChan.Reject(ssh.Prohibited, "specified origin ip or port is invalid")
		return
	}
	dialer := net.Dialer{
		LocalAddr: laddr,
	}

	dconn, err := dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		newChan.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	ch, reqs, err := newChan.Accept()
	if err != nil {
		dconn.Close()
		return
	}
	go ssh.DiscardRequests(reqs)

	go func() {
		defer ch.Close()
		defer dconn.Close()
		io.Copy(ch, dconn)
	}()
	go func() {
		defer ch.Close()
		defer dconn.Close()
		io.Copy(dconn, ch)
	}()
}

func NewSSHServer(addr string) glssh.Server {
	forwardHandler := &glssh.ForwardedTCPHandler{}

	return glssh.Server{
		LocalPortForwardingCallback: glssh.LocalPortForwardingCallback(func(ctx glssh.Context, dhost string, dport uint32) bool {
			log.Println("Accepted forward", dhost, dport)
			return true
		}),
		Addr: addr,
		Handler: glssh.Handler(func(s glssh.Session) {
			io.WriteString(s, "Remote forwarding available...\n")
			select {}
		}),
		ReversePortForwardingCallback: glssh.ReversePortForwardingCallback(func(ctx glssh.Context, host string, port uint32) bool {
			log.Println("attempt to bind", host, port, "granted")
			return true
		}),
		ChannelHandlers: map[string]glssh.ChannelHandler{
			"session":      glssh.DefaultSessionHandler,
			"direct-tcpip": DirectTCPIPHandler,
		},
		RequestHandlers: map[string]glssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
	}
}
