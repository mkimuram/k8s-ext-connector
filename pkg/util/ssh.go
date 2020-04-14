package util

import (
	"context"
	"io"
	"net"
	"sync"

	backoffv4 "github.com/cenkalti/backoff/v4"

	"github.com/golang/glog"
	"golang.org/x/crypto/ssh"
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

func (t *Tunnel) doForward(lCon net.Conn, sCli *ssh.Client) {
	var wg sync.WaitGroup

	rCon, err := sCli.Dial("tcp", t.remoteEndpoint)
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
