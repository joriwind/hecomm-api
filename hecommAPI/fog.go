package hecommAPI

import (
	"crypto/tls"

	"encoding/json"

	"net"

	"fmt"
	"io"

	"log"

	"github.com/joriwind/hecomm-api/hecomm"
)

//FogType Defines the credentials of the fog
const (
	fogAddress string = "192.168.1.123:2000"
)

//RegisterNodes Register the nodes in the fog implementation
func RegisterNodes(nodes []hecomm.DBCNode, config *tls.Config) error {
	conn, err := tls.Dial("tcp", fogAddress, config)
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, node := range nodes {
		nodebytes, err := json.Marshal(node)
		if err != nil {
			return err
		}
		bytes, err := hecomm.NewDBCommand(true, hecomm.ETypeNode, nodebytes)
		if err != nil {
			return err
		}

		log.Printf("Registering node: %v\n", node)
		conn.Write(bytes)
	}

	err = waitForResponse(conn)
	if err != nil {
		return nil
	}

	return nil
}

//RegisterPlatform Register the platform in the fog implementation
func RegisterPlatform(pl hecomm.DBCPlatform, config *tls.Config) error {
	conn, err := tls.Dial("tcp", fogAddress, config)
	if err != nil {
		return err
	}
	defer conn.Close()

	plbytes, err := json.Marshal(pl)
	if err != nil {
		return err
	}
	bytes, err := hecomm.NewDBCommand(true, hecomm.ETypePlatform, plbytes)
	if err != nil {
		return err
	}

	log.Printf("Registering platform: %v\n", pl)

	conn.Write(bytes)

	err = waitForResponse(conn)
	if err != nil {
		return err
	}

	return nil
}

func waitForResponse(conn net.Conn) error {
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		if err == io.EOF {
			return fmt.Errorf("Connection closed by remote")
		}
		return err
	}

	m, err := hecomm.GetMessage(buf[:n])
	if err != nil {
		return err
	}

	rsp, err := m.GetResponse()
	if err != nil {
		return err
	}

	if rsp.OK != true {
		return fmt.Errorf("Did not succeed: %v", rsp)
	}

	return nil
}
