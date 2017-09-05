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
	fogAddress string = "192.168.2.1:2000"
)

//RegisterNodes Register the nodes in the fog implementation
func (pl *Platform) RegisterNodes(nodes []hecomm.DBCNode) error {
	conn, err := tls.Dial("tcp", fogAddress, pl.Config)
	if err != nil {
		return err
	}
	defer conn.Close()

	//Add nodes to hecomm platform server
	for _, val := range nodes {
		log.Printf("Checking deveui: %v\n", val.DevEUI)
		if pl.Nodes == nil {
			log.Fatalln("Unintilized map!")
		}
		if _, ok := pl.Nodes[string(val.DevEUI)]; ok {
			log.Printf("Node already present in hecomm platform server: %v!", string(val.DevEUI))
		} else {
			node := nodeType{DevEUI: val.DevEUI[:]}
			pl.Nodes[string(val.DevEUI)] = &node
		}
	}

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
		return err
	}

	return nil
}

//RegisterPlatform Register the platform in the fog implementation
func (pl *Platform) RegisterPlatform(ple hecomm.DBCPlatform) error {
	conn, err := tls.Dial("tcp", fogAddress, pl.Config)
	if err != nil {
		return err
	}
	defer conn.Close()

	plbytes, err := json.Marshal(ple)
	if err != nil {
		return err
	}
	bytes, err := hecomm.NewDBCommand(true, hecomm.ETypePlatform, plbytes)
	if err != nil {
		return err
	}

	log.Printf("Registering platform: %v\n", ple)

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
	log.Printf("Received %v response\n", rsp.OK)
	if !rsp.OK {
		return fmt.Errorf("Did not succeed: %v", rsp)
	}

	return nil
}
