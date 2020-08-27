// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package endpoint

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/util/retry"
)

var _ Instance = &tcpInstance{}

type tcpInstance struct {
	Config
	l net.Listener
}

func newTCP(config Config) Instance {
	return &tcpInstance{
		Config: config,
	}
}

func (s *tcpInstance) Start(onReady OnReadyFunc) error {
	var listener net.Listener
	var port int
	var err error
	if s.Port.TLS {
		cert, cerr := tls.LoadX509KeyPair(s.TLSCert, s.TLSKey)
		if cerr != nil {
			return fmt.Errorf("could not load TLS keys: %v", cerr)
		}
		config := &tls.Config{Certificates: []tls.Certificate{cert}}
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnPortTLS(s.Port.Port, config)
		// Store the actual listening port back to the argument.
		s.Port.Port = port
	} else {
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnPort(s.Port.Port)
		// Store the actual listening port back to the argument.
		s.Port.Port = port
	}
	if err != nil {
		return err
	}

	s.l = listener
	if s.Port.TLS {
		fmt.Printf("Listening TCP (over TLS) on %v\n", port)
	} else {
		fmt.Printf("Listening TCP on %v\n", port)
	}

	// Start serving TCP traffic.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				epLog.Warn("TCP accept failed: " + err.Error())
				return
			}

			go s.echo(conn)
		}
	}()

	// Notify the WaitGroup once the port has transitioned to ready.
	go s.awaitReady(onReady, port)

	return nil
}

// Handles incoming connection.
func (s *tcpInstance) echo(conn net.Conn) {
	defer common.Metrics.TCPRequests.With(common.PortLabel.Value(strconv.Itoa(s.Port.Port))).Increment()
	defer func() {
		_ = conn.Close()
	}()
	epLog.Infof("TCP Request:\n  Source IP:%s\n  Destination Port:%d", conn.RemoteAddr(), s.Port.Port)

	// If this is server first, client expects a message from server. Send the magic string.
	if s.Port.ServerFirst {
		_, _ = conn.Write([]byte(common.ServerFirstMagicString))
	}

	reader := bufio.NewReader(conn)
	firstReply := true
	for {
		buf, err := reader.ReadBytes(byte('\n'))

		// important not to start sending any response until we've started reading the message,
		// otherwise the response could be read when we expect the magic string
		if firstReply {
			s.writeResponse(conn)
			firstReply = false
		}

		if err != nil {
			if err != io.EOF {
				epLog.Warnf("TCP read failed: %v", err.Error())
			}
			break
		}

		// echo the message from the request
		_, err = conn.Write(buf)
		if err != nil {
			epLog.Warnf("TCP write failed %q, :%v", string(buf), err)
		}
	}
}

func (s *tcpInstance) writeResponse(conn net.Conn) {
	// Write non-request fields specific to the instance
	respFields := map[response.Field]string{
		response.StatusCodeField:     response.StatusCodeOK,
		response.ClusterField:        s.Cluster,
		response.ServiceVersionField: s.Version,
		response.ServicePortField:    strconv.Itoa(s.Port.Port),
	}
	for field, val := range respFields {
		val := fmt.Sprintf("%s=%s\n", string(field), val)
		_, err := conn.Write([]byte(val))
		if err != nil {
			epLog.Warnf("TCP write failed %q: %v", val, err)
		}
	}
}

func (s *tcpInstance) Close() error {
	if s.l != nil {
		s.l.Close()
	}
	return nil
}

func (s *tcpInstance) awaitReady(onReady OnReadyFunc, port int) {
	defer onReady()

	address := fmt.Sprintf("127.0.0.1:%d", port)

	err := retry.UntilSuccess(func() error {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			return err
		}
		defer conn.Close()

		// Server is up now, we're ready.
		return nil
	}, retry.Timeout(readyTimeout), retry.Delay(readyInterval))

	if err != nil {
		epLog.Errorf("readiness failed for endpoint %s: %v", address, err)
	} else {
		epLog.Infof("ready for TCP endpoint %s", address)
	}
}
