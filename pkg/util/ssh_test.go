package util

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestToTCPAddr(t *testing.T) {
	testCases := []struct {
		name        string
		endpoint    string
		portAny     bool
		tcpAddr     *net.TCPAddr
		expectError bool
	}{
		{
			name:     "Normal case (endpoint: 192.168.1.2:2022, portAny: true)",
			endpoint: "192.168.1.2:2022",
			portAny:  true,
			tcpAddr: &net.TCPAddr{
				IP:   net.ParseIP("192.168.1.2"),
				Port: 0,
			},
			expectError: false,
		},
		{
			name:     "Normal case (endpoint: 192.168.1.2:2022, portAny: false)",
			endpoint: "192.168.1.2:2022",
			portAny:  false,
			tcpAddr: &net.TCPAddr{
				IP:   net.ParseIP("192.168.1.2"),
				Port: 2022,
			},
			expectError: false,
		},
		{
			name:        "Error case (Invalid endpoint format, endpoint: 192.168.1.2_2022)",
			endpoint:    "192.168.1.2_2022",
			portAny:     true,
			tcpAddr:     nil,
			expectError: true,
		},
		{
			name:        "Error case (Invalid endpoint port, endpoint: 192.168.1.2:abc)",
			endpoint:    "192.168.1.2:abc",
			portAny:     false,
			tcpAddr:     nil,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		tcpAddr, err := toTCPAddr(tc.endpoint, tc.portAny)
		if !reflect.DeepEqual(tc.tcpAddr, tcpAddr) {
			t.Errorf("expected %v, but got %v", tc.tcpAddr, tcpAddr)
		}
		if tc.expectError && err == nil {
			t.Errorf("expected error, but no error returned")
		}
		if !tc.expectError && err != nil {
			t.Errorf("expected no error, but got error %v", err)
		}
	}
}

func TestString(t *testing.T) {
	testCases := []struct {
		name       string
		localAddr  string
		serverAddr string
		remoteAddr string
		config     *ssh.ClientConfig
		expect     string
	}{
		{
			name:       "Normal case",
			localAddr:  "127.0.0.1:34567",
			serverAddr: "127.0.0.1:45678",
			remoteAddr: "127.0.0.1:56789",
			config:     &ssh.ClientConfig{},
			expect:     "local: 127.0.0.1:34567, server: 127.0.0.1:45678, remote: 127.0.0.1:56789",
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		tun := NewTunnel(tc.localAddr, tc.serverAddr, tc.remoteAddr, tc.config)
		ret := tun.String()
		if tc.expect != ret {
			t.Errorf("expected %s, but got %s", tc.expect, ret)
		}
	}
}

// startEchoServer starts an echo server for test forwarding
// This echo server is just for test and only handle single line in message
func startEchoServer(ctx context.Context, addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer l.Close()

	for {
		select {
		case <-ctx.Done():
			break
		default:
			conn, err := l.Accept()
			if err != nil {
				return err
			}
			if conn == nil {
				return fmt.Errorf("connection is nil")
			}
			defer conn.Close()
			line, err := bufio.NewReader(conn).ReadBytes('\n')
			if err != nil {
				break
			}
			conn.Write(line)
		}
	}

	return nil
}

func genRandomPort() string {
	// range from 32768 to 61000
	return fmt.Sprintf("%d", rand.Intn(61000-32768+1)+32768)
}

// echoClient is a client for the above echo server
// This is just for test and only handle single line in message
// (Add new line on sending message and trim it on returning function)
func echoClient(addr string, msg string) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	conn.Write([]byte(msg + "\n"))
	echo, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(echo), nil
}

func prepareTestServers(ctx context.Context, t *testing.T, echoAddr, sshAddr string, echoDown, sshDown bool) {
	// start echo server
	go func() {
		if echoDown {
			return
		}
		if err := startEchoServer(ctx, echoAddr); err != nil {
			t.Fatal(err)
		}
	}()

	// start ssh server
	sshServer := NewSSHServer(sshAddr)
	go func() {
		if sshDown {
			return
		}
		select {
		case <-ctx.Done():
			sshServer.Close()
		default:
			if err := sshServer.ListenAndServe(); err != nil {
				t.Fatal(err)
			}
			defer sshServer.Close()
		}
	}()

	// Wait for a millisecond for ssh server to be available
	time.Sleep(time.Millisecond)
}

func TestForward(t *testing.T) {
	// Initialize seed
	rand.Seed(time.Now().UnixNano())

	testCases := []struct {
		name        string
		localAddr   string
		serverAddr  string
		remoteAddr  string
		echoDown    bool
		sshDown     bool
		tunnelDown  bool
		config      *ssh.ClientConfig
		msg         string
		expectError bool
	}{
		{
			name:       "Normal case",
			localAddr:  "127.0.0.1:" + genRandomPort(),
			serverAddr: "127.0.0.1:" + genRandomPort(),
			remoteAddr: "127.0.0.1:" + genRandomPort(),
			echoDown:   false,
			sshDown:    false,
			tunnelDown: false,
			config: &ssh.ClientConfig{
				Timeout:         time.Second * 5,
				Auth:            []ssh.AuthMethod{},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			},
			msg:         "hello",
			expectError: false,
		},
		// TODO: fixme, it takes about 120 sec to complete this test
		// x/crypto/ssh: issues/21420 is related?
		/*
			{
				name:       "Error case (echo server down)",
				localAddr:  "127.0.0.1:" + genRandomPort(),
				serverAddr: "127.0.0.1:" + genRandomPort(),
				remoteAddr: "127.0.0.1:" + genRandomPort(),
				// Down
				echoDown:   true,
				sshDown:    false,
				tunnelDown: false,
				config: &ssh.ClientConfig{
					Timeout:         time.Second * 5,
					Auth:            []ssh.AuthMethod{},
					HostKeyCallback: ssh.InsecureIgnoreHostKey(),
				},
				msg: "hello",
				// Should return error
				expectError: true,
			},
		*/
		{
			name:       "Error case (ssh server down)",
			localAddr:  "127.0.0.1:" + genRandomPort(),
			serverAddr: "127.0.0.1:" + genRandomPort(),
			remoteAddr: "127.0.0.1:" + genRandomPort(),
			echoDown:   false,
			// Down
			sshDown:    true,
			tunnelDown: false,
			config: &ssh.ClientConfig{
				Timeout:         time.Second * 5,
				Auth:            []ssh.AuthMethod{},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			},
			msg: "hello",
			// Should return error
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		ctx, cancel := context.WithCancel(context.Background())

		// start echo server on remoteAddr and ssh server on serverAddr
		prepareTestServers(ctx, t, tc.remoteAddr, tc.serverAddr, tc.echoDown, tc.sshDown)

		// start tunnel to forward remoteAddr to localAddr
		tun := NewTunnel(tc.localAddr, tc.serverAddr, tc.remoteAddr, tc.config)
		go func() {
			select {
			case <-ctx.Done():
				tun.Cancel()
			default:
				if err := tun.Forward(); err != nil {
					return
				}
				defer tun.Cancel()
			}
		}()

		// Wait for two seconds for tunnel to be available
		time.Sleep(2 * time.Second)
		// test connecting to forwarded localAddr
		msg, err := echoClient(tc.localAddr, tc.msg)
		if tc.expectError {
			if err == nil {
				t.Errorf("expected error, but no error returned")
			}
		} else {
			if err != nil {
				t.Errorf("expected no error, but got error %v", err)
			}
			if tc.msg != msg {
				t.Errorf("expected msg %s, but got %s", tc.msg, msg)
			}
		}

		// Cancel servers and tunnel
		cancel()
		// Wait for a millisecond just to be sure that all servers closed
		time.Sleep(time.Millisecond)
	}
}

func TestRemoteForward(t *testing.T) {
	// Initialize seed
	rand.Seed(time.Now().UnixNano())
	testCases := []struct {
		name        string
		localAddr   string
		serverAddr  string
		remoteAddr  string
		echoDown    bool
		sshDown     bool
		tunnelDown  bool
		config      *ssh.ClientConfig
		msg         string
		expectError bool
	}{
		{
			name:       "Normal case",
			localAddr:  "127.0.0.1:" + genRandomPort(),
			serverAddr: "127.0.0.1:" + genRandomPort(),
			remoteAddr: "127.0.0.1:" + genRandomPort(),
			echoDown:   false,
			sshDown:    false,
			tunnelDown: false,
			config: &ssh.ClientConfig{
				Auth:            []ssh.AuthMethod{},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			},
			msg:         "hello",
			expectError: false,
		},
		{
			name:       "Error case (echo server down)",
			localAddr:  "127.0.0.1:" + genRandomPort(),
			serverAddr: "127.0.0.1:" + genRandomPort(),
			remoteAddr: "127.0.0.1:" + genRandomPort(),
			// Down
			echoDown:   true,
			sshDown:    false,
			tunnelDown: false,
			config: &ssh.ClientConfig{
				Auth:            []ssh.AuthMethod{},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			},
			msg: "hello",
			// Should return error
			expectError: true,
		},
		{
			name:       "Error case (ssh server down)",
			localAddr:  "127.0.0.1:" + genRandomPort(),
			serverAddr: "127.0.0.1:" + genRandomPort(),
			remoteAddr: "127.0.0.1:" + genRandomPort(),
			echoDown:   false,
			// Down
			sshDown:    true,
			tunnelDown: false,
			config: &ssh.ClientConfig{
				Auth:            []ssh.AuthMethod{},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			},
			msg: "hello",
			// Should return error
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		ctx, cancel := context.WithCancel(context.Background())

		// start echo server on localAddr and ssh server on serverAddr
		prepareTestServers(ctx, t, tc.localAddr, tc.serverAddr, tc.echoDown, tc.sshDown)

		// start tunnel to remoteForward localAddr to remoteAddr
		tun := NewTunnel(tc.localAddr, tc.serverAddr, tc.remoteAddr, tc.config)
		go func() {
			select {
			case <-ctx.Done():
				tun.Cancel()
			default:
				if err := tun.RemoteForward(); err != nil {
					return
				}
				defer tun.Cancel()
			}
		}()

		// Wait for two seconds for tunnel to be available
		time.Sleep(2 * time.Second)

		// test connecting to forwarded remoteAddr
		msg, err := echoClient(tc.remoteAddr, tc.msg)
		if tc.expectError {
			if err == nil {
				t.Errorf("expected error, but no error returned")
			}
		} else {
			if err != nil {
				t.Errorf("expected no error, but got error %v", err)
			}
			if tc.msg != msg {
				t.Errorf("expected msg %s, but got %s", tc.msg, msg)
			}
		}

		// Cancel servers and tunnel
		cancel()
		// Wait for a millisecond just to be sure that all servers closed
		time.Sleep(time.Millisecond)
	}
}

func TestIsPortOpen(t *testing.T) {
	testCases := []struct {
		name     string
		ip       string
		port     string
		expected bool
	}{
		{
			name:     "Normal case: test 127.0.0.1:34567",
			ip:       "127.0.0.1",
			port:     "34567",
			expected: true,
		},
		{
			name:     "Error case: test 127.0.0.1:34567",
			ip:       "127.0.0.1",
			port:     "34567",
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		done := make(chan bool)
		defer close(done)

		// open port if tc.expected
		go func() {
			if tc.expected {
				l, err := net.Listen("tcp", net.JoinHostPort(tc.ip, tc.port))
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			}
			// wait for IsPortOpen
			<-done
		}()

		// Wait for a millisecond just to be sure that the port opened
		time.Sleep(time.Millisecond)

		ret := IsPortOpen(tc.ip, tc.port)
		done <- true

		if tc.expected != ret {
			t.Errorf("expected %v, but got %v", tc.expected, ret)
		}

		// Wait for a millisecond just to be sure that the port closed
		time.Sleep(time.Millisecond)
	}
}
