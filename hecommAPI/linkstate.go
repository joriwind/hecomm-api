package hecommAPI

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"github.com/joriwind/hecomm-api/hecomm"
)

type keyListener struct {
	channel chan []byte
}

func newKeyListener(keychannel chan []byte) *keyListener {
	return &keyListener{
		channel: keychannel,
	}
}

func (k keyListener) Write(p []byte) (n int, err error) {
	fmt.Printf("Got key: %v!\n", string(p))
	//Ommit the "CLIENT_RANDOM "
	go func() { k.channel <- p[14+64+1 : len(p)-1] }()
	return 0, nil
}

type linkstConn struct {
	conn net.Conn
	data *bytes.Buffer
}

func newLinkstConn(connection net.Conn) *linkstConn {
	return &linkstConn{
		conn: connection,
		data: bytes.NewBuffer(make([]byte, 0, 8000)),
	}
}

func (c linkstConn) Read(b []byte) (n int, err error) {
	buf := make([]byte, 4000)
	s := 0
	var message *hecomm.Message

	if c.data.Len() > 0 {
		n, err = c.data.Read(b)
		fmt.Printf("lsc %v read %v bytes\n", c.conn.LocalAddr(), n)
		return n, err
	}

	for {
		n, err = c.conn.Read(buf[s:])
		if err != nil {
			//log.Fatalf("Could not read from connection: %v\n", err)
			return 0, err
		}
		//fmt.Printf("lsc %v: reading: %v\n", c.conn.LocalAddr(), n)

		if buf[0] != 123 {
			return 0, fmt.Errorf("First character != 123: %v", buf[0])
		}

		if buf[n+s-1] == 125 {
			message, err = hecomm.GetMessage(buf[:n+s])
			if err != nil {
				fmt.Printf("Unable to decompile message: %v", buf[:n])
				return 0, err
			}
			if message.FPort != hecomm.FPortLinkState {
				return 0, fmt.Errorf("Not the expected fPort != FPortLinkState")
			}
			s = 0
			fmt.Printf("Finished compiling packet %v bytes\n", len(message.Data[:]))
			//Add to buffer

			ex, err := c.data.Write(message.Data)
			if err != nil {
				return 0, err
			}
			fmt.Printf("Added: %v bytes to buffer\n", ex)
			break
		} else {
			s = s + n
		}
	}

	/* if len(b) < len(message.Data) {
		fmt.Printf("Buffer to small: %v vs %v", len(b), len(message.Data))

		return len(message.Data), nil
	} */

	n, err = c.data.Read(b)
	fmt.Printf("lsc %v read %v bytes\n", c.conn.LocalAddr(), n)
	return n, err

}

func (c linkstConn) Write(b []byte) (n int, err error) {
	bytes, err := hecomm.NewMessage(hecomm.FPortLinkState, b)
	if err != nil {
		//log.Fatalf("Could not create hecomm message: %v\n", err)
		fmt.Printf("linkstateconnection: Unable to create message\n")
		return 0, err
	}
	//fmt.Printf("lsc %v: writing: %v, data: %v\n", c.conn.LocalAddr(), len(bytes), bytes)
	n, err = c.conn.Write(bytes)

	fmt.Printf("lsc %v: written: %v\n", c.conn.LocalAddr(), n)
	return n, err
}

func (c linkstConn) Close() error {
	return c.conn.Close()
}

func (c linkstConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c linkstConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c linkstConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c linkstConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c linkstConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
